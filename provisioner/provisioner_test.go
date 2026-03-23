package provisioner

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testYAML = `
kind: Stream
name: TEST_STREAM
subjects:
  - "test.*"
storage: memory
max_age: 1h

---
kind: Consumer
name: TEST_CONS
streamName: TEST_STREAM
deliver_policy: all
ack_wait: 30s

---
kind: KeyValue
name: TEST_KV
history: 3
storage: memory

---
kind: ObjectStore
name: TEST_OBJ
description: "Test Object Store"
storage: memory
`

func runTestServer(t *testing.T) *server.Server {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "nats-provisioner-test-")
	require.NoError(t, err)

	opts := &natsserver.DefaultTestOptions
	opts.JetStream = true
	opts.Port = -1
	opts.StoreDir = tempDir

	srv := natsserver.RunServer(opts)
	require.NotNil(t, srv, "NATS server failed to start")

	t.Cleanup(func() {
		os.RemoveAll(tempDir)
	})

	return srv
}

func TestProvisioner(t *testing.T) {
	srv := runTestServer(t)
	defer srv.Shutdown()

	tmpFile, err := os.CreateTemp("", "nats-config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(testYAML)
	require.NoError(t, err)
	tmpFile.Close()

	prov, err := NewProvisioner(srv.ClientURL(), "", "", "")
	require.NoError(t, err)
	defer prov.Close()

	ctx := context.Background()

	err = prov.ProvisionFile(ctx, tmpFile.Name())
	require.NoError(t, err, "ProvisionFile should not error")

	nc, err := nats.Connect(srv.ClientURL())
	require.NoError(t, err)
	defer nc.Close()
	js, err := jetstream.New(nc)
	require.NoError(t, err)

	stream, err := js.Stream(ctx, "TEST_STREAM")
	require.NoError(t, err, "Stream should exist")
	sInfo, _ := stream.Info(ctx)
	assert.Equal(t, jetstream.MemoryStorage, sInfo.Config.Storage)
	assert.Equal(t, time.Hour, sInfo.Config.MaxAge)
	assert.Contains(t, sInfo.Config.Subjects, "test.*")

	cons, err := stream.Consumer(ctx, "TEST_CONS")
	require.NoError(t, err, "Consumer should exist")
	cInfo, _ := cons.Info(ctx)
	assert.Equal(t, jetstream.DeliverAllPolicy, cInfo.Config.DeliverPolicy)
	assert.Equal(t, 30*time.Second, cInfo.Config.AckWait)

	kv, err := js.KeyValue(ctx, "TEST_KV")
	require.NoError(t, err, "KV should exist")
	kvStatus, _ := kv.Status(ctx)
	assert.Equal(t, int64(3), kvStatus.History())

	obj, err := js.ObjectStore(ctx, "TEST_OBJ")
	require.NoError(t, err, "ObjectStore should exist")
	objStatus, _ := obj.Status(ctx)
	assert.Equal(t, "Test Object Store", objStatus.Description())

	orphans, err := prov.DetectOrphans(ctx)
	require.NoError(t, err)
	assert.Empty(t, orphans, "Should have exactly zero orphans after provisioning")

	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     "MANUAL_STREAM",
		Subjects: []string{"manual.>"},
	})
	require.NoError(t, err)

	orphans, err = prov.DetectOrphans(ctx)
	require.NoError(t, err)
	assert.Len(t, orphans, 1, "Should detect exactly 1 orphan")
	assert.Equal(t, "Stream: MANUAL_STREAM", orphans[0])
}
