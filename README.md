# NATS Provisioner

**NATS Provisioner** is a lightweight, dependency-free CLI tool for declaratively managing NATS JetStream resources (Streams, Consumers, KeyValue stores, and ObjectStores). 

Inspired by [NACK (NATS Controllers for Kubernetes)](https://github.com/nats-io/nack), this tool is designed specifically for **non-Kubernetes environments** (Bare metal, VMs, Nomad, or Docker) and **CI/CD pipelines**. It reads multi-document YAML files—either from a local path or directly from a Git repository—and applies them idempotently to your NATS cluster.

Especially useful for developers to provision a local NATS server or non Kubernetes environments.

## Features

- **No Kubernetes Required**: Pure Go CLI. No CRDs, no Kube API server.
- **Idempotent**: Safely run it on every commit. It creates resources if they don't exist and updates them if they do.
- **GitOps Native**: Can clone configurations directly from private or public Git repositories using memory/temporary storage.
- **Secure Auth**: Supports NKey, Username/Password, and TLS connections.
- **Zero-Lag Schema**: Because it unmarshals YAML directly into the official `nats.go` configuration structs, it automatically supports **100% of all new NATS features** the moment you update the library. No waiting for tool maintainers to add support for new fields!


## Usage

The tool accepts several flags to define how to connect to NATS and where to find the YAML configurations.

### Local Files

You can point the tool to a single file or a directory containing multiple `.yaml` / `.yml` files.

```bash
nats-provisioner -s nats://localhost:4222 -path ./nats-configs/
```

### Git Repository

To provision directly from a Git repository, provide the Git URL. The tool will clone the repository to a temporary directory, process all `.yaml` files, and clean up after itself.

```bash
# Public repository
nats-provisioner -s nats://localhost:4222 \
  -git-url https://github.com/my-org/nats-infra.git

# Private repository (using Personal Access Token)
nats-provisioner -s nats://localhost:4222 \
  -git-url https://github.com/my-org/nats-infra.git \
  -git-user my-ci-bot \
  -git-pass github_pat_11ABCD...
```

## Authentication

Connect to secure NATS clusters using either NKey or Username/Password.

**Using NKey (Recommended):**
```bash
nats-provisioner -s tls://nats.production.io:4222 \
  -nkey UAX... \
  -path ./configs.yaml
```

**Using User/Password:**
```bash
nats-provisioner -s tls://nats.production.io:4222 \
  -user admin \
  -pass s3cr3t \
  -path ./configs.yaml
```

## Configuration Format

Define resources in simple YAML documents. Separate multiple resources with `---`. 

*Note: Time durations should be formatted as Go duration strings (e.g., `1s`, `2m`, `1h`, `24h`).*

### Stream
Streams define message storage. They hold messages and enforce limits like size, age, and message counts.

| Field | Type | Allowed Values | Description |
|---|---|---|---|
| `name` | string | | The unique name of the stream. |
| `description` | string | | A human-readable description of the stream. |
| `subjects` | []string | | A list of NATS subjects to consume from (wildcards `*` and `>` supported). |
| `storage` | string | `file` (default), `memory` | The storage backend to use for the Stream. |
| `retention` | string | `limits` (default), `interest`, `workqueue` | How messages are retained. `limits`: keep until limits reached. `interest`: keep until all known consumers ack. `workqueue`: delete after first ack. |
| `discard` | string | `old` (default), `new` | Behavior when limits are reached. `old`: delete oldest messages. `new`: reject new messages. |
| `max_age` | duration | e.g., `1h`, `24h` | Maximum age of any message in the stream. |
| `max_bytes` | int64 | | Maximum total size of the stream in bytes (`-1` for unlimited). |
| `max_msgs` | int64 | | Maximum number of messages in the stream (`-1` for unlimited). |
| `max_msg_size` | int32 | | Maximum size of a single message in bytes (`-1` for unlimited). |
| `max_consumers` | int | | Maximum number of consumers allowed on the stream. |
| `max_msgs_per_subject`| int64 | | Maximum number of messages retained per unique subject. |
| `replicas` | int | `1` to `5` | Number of JetStream cluster replicas for this stream. |
| `no_ack` | bool | `true`, `false` | If `true`, disables acknowledgements for messages received by this stream. |

**Example:**
```yaml
kind: Stream
name: ORDERS
subjects:
  - "orders.*"
storage: file
retention: limits
max_age: 24h
replicas: 3
```

### Consumer
Consumers act as stateful views into a Stream, tracking which messages have been delivered and acknowledged.

| Field | Type | Allowed Values | Description |
|---|---|---|---|
| `name` | string | | The durable name for the consumer. |
| `streamName` | string | | **(Required)** The name of the Stream this consumer is bound to. |
| `description` | string | | A human-readable description. |
| `deliver_policy` | string | `all` (default), `last`, `new`, `by_start_sequence`, `by_start_time` | Where to start delivering messages from. |
| `ack_policy` | string | `explicit` (default), `none`, `all` | How messages should be acknowledged. |
| `ack_wait` | duration | e.g., `30s` | How long the server waits for an ack before redelivering the message. |
| `max_deliver` | int | | Maximum number of delivery attempts for a message before it is dropped/termed. |
| `filter_subject` | string | | Select only specific incoming subjects from the stream (supports wildcards). |
| `replay_policy` | string | `instant` (default), `original` | `instant`: deliver as fast as possible. `original`: match the timing of when messages were published. |
| `max_waiting` | int | | Maximum number of active pull requests allowed. |
| `max_ack_pending`| int | | Maximum number of unacknowledged messages allowed in flight. |

**Example:**
```yaml
kind: Consumer
name: orders-processor
streamName: ORDERS
deliver_policy: all
ack_policy: explicit
ack_wait: 30s
max_deliver: 5
filter_subject: "orders.us.>"
```

### KeyValue
A Key-Value store backed by a JetStream stream.

| Field | Type | Allowed Values | Description |
|---|---|---|---|
| `name` | string | | The unique name for the KV Store. |
| `description` | string | | A human-readable description. |
| `storage` | string | `file` (default), `memory` | The storage backend to use for the KV Store. |
| `history` | uint8 | `1` to `64` | The number of historical values to keep per key. |
| `ttl` | duration | e.g., `2h` | The expiry time for keys. Keys older than this will be deleted. |
| `max_bytes` | int64 | | Maximum size of the KV Store in bytes. |
| `max_value_size`| int32 | | Maximum size of a single value in bytes. |
| `replicas` | int | `1` to `5` | The number of replicas to keep in clustered JetStream. |

**Example:**
```yaml
kind: KeyValue
name: APP_CONFIG
description: "Global Application Configurations"
history: 5
ttl: 720h
storage: memory
replicas: 3
```

### ObjectStore
An Object Store optimized for large files/blobs, backed by JetStream.

| Field | Type | Allowed Values | Description |
|---|---|---|---|
| `name` | string | | The unique name for the Object Store. |
| `description` | string | | A human-readable description. |
| `storage` | string | `file` (default), `memory` | The storage backend to use for the Object Store. |
| `ttl` | duration | e.g., `24h` | The expiry time for objects. Objects older than this will be deleted. |
| `max_bytes` | int64 | | Maximum size of the Object Store in bytes. |
| `replicas` | int | `1` to `5` | The number of replicas to keep in clustered JetStream. |

**Example:**
```yaml
kind: ObjectStore
name: PROFILE_PICTURES
description: "User uploaded profile images"
ttl: 8760h
storage: file
replicas: 3
```

## License

This project is licensed under the MIT License.