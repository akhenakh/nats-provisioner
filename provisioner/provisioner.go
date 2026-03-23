package provisioner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"gopkg.in/yaml.v3"
)

type ExpectedResources struct {
	Streams      map[string]bool
	Consumers    map[string]map[string]bool
	KeyValues    map[string]bool
	ObjectStores map[string]bool
}

type Provisioner struct {
	js       jetstream.JetStream
	nc       *nats.Conn
	expected ExpectedResources
}

func NewProvisioner(url, nkey, user, password string) (*Provisioner, error) {
	opts := []nats.Option{
		nats.Name("nats-provisioner"),
		nats.Timeout(10 * time.Second),
	}

	if nkey != "" {
		opt, err := nats.NkeyOptionFromSeed(nkey)
		if err != nil {
			return nil, fmt.Errorf("invalid nkey seed: %w", err)
		}
		opts = append(opts, opt)
	}

	if user != "" && password != "" {
		opts = append(opts, nats.UserInfo(user, password))
	}

	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("nats connection failed: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream initialization failed: %w", err)
	}

	return &Provisioner{
		js: js,
		nc: nc,
		expected: ExpectedResources{
			Streams:      make(map[string]bool),
			Consumers:    make(map[string]map[string]bool),
			KeyValues:    make(map[string]bool),
			ObjectStores: make(map[string]bool),
		},
	}, nil
}

func (p *Provisioner) Close() {
	if p.nc != nil {
		p.nc.Close()
	}
}

func (p *Provisioner) ProvisionFile(ctx context.Context, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var wrapper ResourceWrapper
		err := decoder.Decode(&wrapper)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to decode YAML in %s: %w", path, err)
		}

		if wrapper.Kind == "" {
			continue
		}

		if err := p.applyResource(ctx, wrapper); err != nil {
			return fmt.Errorf("failed to apply %s '%s': %w", wrapper.Kind, wrapper.Metadata.Name, err)
		}
	}
	return nil
}

func (p *Provisioner) applyResource(ctx context.Context, res ResourceWrapper) error {
	switch res.Kind {
	case "Stream":
		var spec StreamSpec
		if err := res.Spec.Decode(&spec); err != nil {
			return err
		}
		if spec.Name == "" {
			spec.Name = res.Metadata.Name
		}
		p.expected.Streams[spec.Name] = true

		cfg, err := toStreamConfig(spec)
		if err != nil {
			return err
		}
		return p.applyStream(ctx, cfg)

	case "Consumer":
		var spec ConsumerSpec
		if err := res.Spec.Decode(&spec); err != nil {
			return err
		}
		if spec.Durable == "" {
			spec.Durable = res.Metadata.Name
		}
		if p.expected.Consumers[spec.StreamName] == nil {
			p.expected.Consumers[spec.StreamName] = make(map[string]bool)
		}
		p.expected.Consumers[spec.StreamName][spec.Durable] = true

		cfg, err := toConsumerConfig(spec)
		if err != nil {
			return err
		}
		return p.applyConsumer(ctx, spec.StreamName, cfg)

	case "KeyValue":
		var spec KeyValueSpec
		if err := res.Spec.Decode(&spec); err != nil {
			return err
		}
		if spec.Bucket == "" {
			spec.Bucket = res.Metadata.Name
		}
		p.expected.KeyValues[spec.Bucket] = true

		cfg, err := toKeyValueConfig(spec)
		if err != nil {
			return err
		}
		return p.applyKeyValue(ctx, cfg)

	case "ObjectStore":
		var spec ObjectStoreSpec
		if err := res.Spec.Decode(&spec); err != nil {
			return err
		}
		if spec.Bucket == "" {
			spec.Bucket = res.Metadata.Name
		}
		p.expected.ObjectStores[spec.Bucket] = true

		cfg, err := toObjectStoreConfig(spec)
		if err != nil {
			return err
		}
		return p.applyObjectStore(ctx, cfg)

	default:
		log.Printf("Warning: Unknown kind '%s'. Skipping.", res.Kind)
		return nil
	}
}

func (p *Provisioner) applyStream(ctx context.Context, cfg jetstream.StreamConfig) error {
	_, err := p.js.Stream(ctx, cfg.Name)
	if errors.Is(err, jetstream.ErrStreamNotFound) {
		log.Printf("Creating Stream: %s", cfg.Name)
		_, err = p.js.CreateStream(ctx, cfg)
		return err
	} else if err != nil {
		return err
	}
	log.Printf("Updating Stream: %s", cfg.Name)
	_, err = p.js.UpdateStream(ctx, cfg)
	return err
}

func (p *Provisioner) applyConsumer(ctx context.Context, streamName string, cfg jetstream.ConsumerConfig) error {
	if streamName == "" {
		return errors.New("streamName must be provided for a consumer")
	}

	_, err := p.js.Consumer(ctx, streamName, cfg.Durable)
	if errors.Is(err, jetstream.ErrConsumerNotFound) {
		log.Printf("Creating Consumer: %s on Stream: %s", cfg.Durable, streamName)
		_, err = p.js.CreateOrUpdateConsumer(ctx, streamName, cfg)
		return err
	} else if err != nil {
		return err
	}

	log.Printf("Updating Consumer: %s on Stream: %s", cfg.Durable, streamName)
	_, err = p.js.CreateOrUpdateConsumer(ctx, streamName, cfg)
	return err
}

func (p *Provisioner) applyKeyValue(ctx context.Context, cfg jetstream.KeyValueConfig) error {
	_, err := p.js.KeyValue(ctx, cfg.Bucket)
	if errors.Is(err, jetstream.ErrBucketNotFound) {
		log.Printf("Creating KeyValue: %s", cfg.Bucket)
		_, err = p.js.CreateKeyValue(ctx, cfg)
		return err
	} else if err != nil {
		return err
	}

	log.Printf("Updating KeyValue: %s", cfg.Bucket)
	_, err = p.js.UpdateKeyValue(ctx, cfg)
	return err
}

func (p *Provisioner) applyObjectStore(ctx context.Context, cfg jetstream.ObjectStoreConfig) error {
	_, err := p.js.ObjectStore(ctx, cfg.Bucket)
	if errors.Is(err, jetstream.ErrBucketNotFound) {
		log.Printf("Creating ObjectStore: %s", cfg.Bucket)
		_, err = p.js.CreateObjectStore(ctx, cfg)
		return err
	} else if err != nil {
		return err
	}

	log.Printf("Updating ObjectStore: %s", cfg.Bucket)
	_, err = p.js.UpdateObjectStore(ctx, cfg)
	return err
}

// DetectOrphans returns a list of resource names that exist on NATS but aren't in the YAML
func (p *Provisioner) DetectOrphans(ctx context.Context) ([]string, error) {
	var orphans []string

	streamLister := p.js.ListStreams(ctx)
	for info := range streamLister.Info() {
		streamName := info.Config.Name
		isSystemStream := false

		if strings.HasPrefix(streamName, "KV_") {
			bucket := strings.TrimPrefix(streamName, "KV_")
			if !p.expected.KeyValues[bucket] {
				orphans = append(orphans, fmt.Sprintf("KeyValue: %s", bucket))
			}
			isSystemStream = true
		} else if strings.HasPrefix(streamName, "OBJ_") {
			bucket := strings.TrimPrefix(streamName, "OBJ_")
			if !p.expected.ObjectStores[bucket] {
				orphans = append(orphans, fmt.Sprintf("ObjectStore: %s", bucket))
			}
			isSystemStream = true
		} else {
			if !p.expected.Streams[streamName] {
				orphans = append(orphans, fmt.Sprintf("Stream: %s", streamName))
			}
		}

		if !isSystemStream {
			stream, err := p.js.Stream(ctx, streamName)
			if err != nil {
				continue
			}

			consLister := stream.ListConsumers(ctx)
			for consInfo := range consLister.Info() {
				consName := consInfo.Name
				if p.expected.Consumers[streamName] == nil || !p.expected.Consumers[streamName][consName] {
					orphans = append(orphans, fmt.Sprintf("Consumer: %s (Stream: %s)", consName, streamName))
				}
			}
		}
	}

	if err := streamLister.Err(); err != nil {
		return nil, fmt.Errorf("failed to list streams: %w", err)
	}

	return orphans, nil
}
