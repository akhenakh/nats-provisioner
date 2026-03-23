package provisioner

import (
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"gopkg.in/yaml.v3"
)

type ResourceWrapper struct {
	ApiVersion string    `yaml:"apiVersion"`
	Kind       string    `yaml:"kind"`
	Metadata   Metadata  `yaml:"metadata"`
	Spec       yaml.Node `yaml:"spec"`
}

type Metadata struct {
	Name string `yaml:"name"`
}

type StreamSpec struct {
	Name              string   `yaml:"name"`
	Subjects          []string `yaml:"subjects"`
	Storage           string   `yaml:"storage"`
	Retention         string   `yaml:"retention"`
	MaxAge            string   `yaml:"max_age"`
	MaxBytes          int64    `yaml:"max_bytes"`
	MaxMsgs           int64    `yaml:"max_msgs"`
	MaxMsgSize        int32    `yaml:"max_msg_size"`
	MaxConsumers      int      `yaml:"max_consumers"`
	MaxMsgsPerSubject int64    `yaml:"max_msgs_per_subject"`
	Replicas          int      `yaml:"replicas"`
	Discard           string   `yaml:"discard"`
	Description       string   `yaml:"description"`
	NoAck             bool     `yaml:"no_ack"`
}

type ConsumerSpec struct {
	StreamName    string `yaml:"streamName"`
	Durable       string `yaml:"durable_name"`
	Description   string `yaml:"description"`
	DeliverPolicy string `yaml:"deliver_policy"`
	AckPolicy     string `yaml:"ack_policy"`
	AckWait       string `yaml:"ack_wait"`
	MaxDeliver    int    `yaml:"max_deliver"`
	FilterSubject string `yaml:"filter_subject"`
	ReplayPolicy  string `yaml:"replay_policy"`
	MaxWaiting    int    `yaml:"max_waiting"`
	MaxAckPending int    `yaml:"max_ack_pending"`
}

type KeyValueSpec struct {
	Bucket       string `yaml:"bucket"`
	Description  string `yaml:"description"`
	MaxValueSize int32  `yaml:"max_value_size"`
	History      uint8  `yaml:"history"`
	TTL          string `yaml:"ttl"`
	MaxBytes     int64  `yaml:"max_bytes"`
	Storage      string `yaml:"storage"`
	Replicas     int    `yaml:"replicas"`
}

type ObjectStoreSpec struct {
	Bucket      string `yaml:"bucket"`
	Description string `yaml:"description"`
	TTL         string `yaml:"ttl"`
	MaxBytes    int64  `yaml:"max_bytes"`
	Storage     string `yaml:"storage"`
	Replicas    int    `yaml:"replicas"`
}

func toStreamConfig(spec StreamSpec) (jetstream.StreamConfig, error) {
	cfg := jetstream.StreamConfig{
		Name:              spec.Name,
		Subjects:          spec.Subjects,
		MaxBytes:          spec.MaxBytes,
		MaxMsgs:           spec.MaxMsgs,
		MaxMsgSize:        spec.MaxMsgSize,
		MaxConsumers:      spec.MaxConsumers,
		MaxMsgsPerSubject: spec.MaxMsgsPerSubject,
		Replicas:          spec.Replicas,
		Description:       spec.Description,
		NoAck:             spec.NoAck,
	}

	switch strings.ToLower(spec.Storage) {
	case "memory":
		cfg.Storage = jetstream.MemoryStorage
	case "file", "":
		cfg.Storage = jetstream.FileStorage
	default:
		return cfg, fmt.Errorf("invalid storage type: %s", spec.Storage)
	}

	switch strings.ToLower(spec.Retention) {
	case "interest":
		cfg.Retention = jetstream.InterestPolicy
	case "workqueue":
		cfg.Retention = jetstream.WorkQueuePolicy
	case "limits", "":
		cfg.Retention = jetstream.LimitsPolicy
	default:
		return cfg, fmt.Errorf("invalid retention policy: %s", spec.Retention)
	}

	switch strings.ToLower(spec.Discard) {
	case "new":
		cfg.Discard = jetstream.DiscardNew
	case "old", "":
		cfg.Discard = jetstream.DiscardOld
	default:
		return cfg, fmt.Errorf("invalid discard policy: %s", spec.Discard)
	}

	if spec.MaxAge != "" {
		d, err := time.ParseDuration(spec.MaxAge)
		if err != nil {
			return cfg, fmt.Errorf("invalid max_age format: %w", err)
		}
		cfg.MaxAge = d
	}

	return cfg, nil
}

func toConsumerConfig(spec ConsumerSpec) (jetstream.ConsumerConfig, error) {
	cfg := jetstream.ConsumerConfig{
		Durable:       spec.Durable,
		Description:   spec.Description,
		MaxDeliver:    spec.MaxDeliver,
		FilterSubject: spec.FilterSubject,
		MaxWaiting:    spec.MaxWaiting,
		MaxAckPending: spec.MaxAckPending,
	}

	switch strings.ToLower(spec.DeliverPolicy) {
	case "all", "":
		cfg.DeliverPolicy = jetstream.DeliverAllPolicy
	case "last":
		cfg.DeliverPolicy = jetstream.DeliverLastPolicy
	case "new":
		cfg.DeliverPolicy = jetstream.DeliverNewPolicy
	case "by_start_sequence":
		cfg.DeliverPolicy = jetstream.DeliverByStartSequencePolicy
	case "by_start_time":
		cfg.DeliverPolicy = jetstream.DeliverByStartTimePolicy
	default:
		return cfg, fmt.Errorf("invalid deliver_policy: %s", spec.DeliverPolicy)
	}

	switch strings.ToLower(spec.AckPolicy) {
	case "none":
		cfg.AckPolicy = jetstream.AckNonePolicy
	case "all":
		cfg.AckPolicy = jetstream.AckAllPolicy
	case "explicit", "":
		cfg.AckPolicy = jetstream.AckExplicitPolicy
	default:
		return cfg, fmt.Errorf("invalid ack_policy: %s", spec.AckPolicy)
	}

	switch strings.ToLower(spec.ReplayPolicy) {
	case "original":
		cfg.ReplayPolicy = jetstream.ReplayOriginalPolicy
	case "instant", "":
		cfg.ReplayPolicy = jetstream.ReplayInstantPolicy
	default:
		return cfg, fmt.Errorf("invalid replay_policy: %s", spec.ReplayPolicy)
	}

	if spec.AckWait != "" {
		d, err := time.ParseDuration(spec.AckWait)
		if err != nil {
			return cfg, fmt.Errorf("invalid ack_wait format: %w", err)
		}
		cfg.AckWait = d
	}

	return cfg, nil
}

func toKeyValueConfig(spec KeyValueSpec) (jetstream.KeyValueConfig, error) {
	cfg := jetstream.KeyValueConfig{
		Bucket:       spec.Bucket,
		Description:  spec.Description,
		MaxValueSize: spec.MaxValueSize,
		History:      spec.History,
		MaxBytes:     spec.MaxBytes,
		Replicas:     spec.Replicas,
	}

	switch strings.ToLower(spec.Storage) {
	case "memory":
		cfg.Storage = jetstream.MemoryStorage
	case "file", "":
		cfg.Storage = jetstream.FileStorage
	default:
		return cfg, fmt.Errorf("invalid storage type: %s", spec.Storage)
	}

	if spec.TTL != "" {
		d, err := time.ParseDuration(spec.TTL)
		if err != nil {
			return cfg, fmt.Errorf("invalid ttl format: %w", err)
		}
		cfg.TTL = d
	}

	return cfg, nil
}

func toObjectStoreConfig(spec ObjectStoreSpec) (jetstream.ObjectStoreConfig, error) {
	cfg := jetstream.ObjectStoreConfig{
		Bucket:      spec.Bucket,
		Description: spec.Description,
		MaxBytes:    spec.MaxBytes,
		Replicas:    spec.Replicas,
	}

	switch strings.ToLower(spec.Storage) {
	case "memory":
		cfg.Storage = jetstream.MemoryStorage
	case "file", "":
		cfg.Storage = jetstream.FileStorage
	default:
		return cfg, fmt.Errorf("invalid storage type: %s", spec.Storage)
	}

	if spec.TTL != "" {
		d, err := time.ParseDuration(spec.TTL)
		if err != nil {
			return cfg, fmt.Errorf("invalid ttl format: %w", err)
		}
		cfg.TTL = d
	}

	return cfg, nil
}
