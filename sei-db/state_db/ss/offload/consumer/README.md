# historical-offload-consumer

Reads `ChangelogEntry` messages from the Kafka topic cryptosim publishes to and writes them to CockroachDB.

## Layout

- `schema/schema.sql` — CockroachDB DDL (idempotent)
- `cmd/historical-offload-consumer/` — CLI binary
- `config/example.json` — sample config
- `deploy.sh` — one-shot setup helper

## Cloud prerequisites (manual)

- MSK cluster + topic + IAM role with `kafka-cluster:Connect` and read on the topic
- CockroachDB cluster + database + user
- AWS credentials available to the process (env or IAM role)

## Run

```bash
export KAFKA_BROKERS="b-1...:9098,b-2...:9098"
export KAFKA_TOPIC="historical-offload"
export KAFKA_GROUP_ID="historical-offload-consumer"
export AWS_REGION="us-east-1"
export COCKROACH_DSN="postgresql://user@host:26257/db?sslmode=verify-full"

RUN=1 ./deploy.sh
```

`deploy.sh` applies the schema, writes the config, builds the binary, and (with `RUN=1`) starts it. Flags: `SKIP_SCHEMA=1`, `SKIP_BUILD=1`.

## Guarantees

- At-least-once delivery. Sink UPSERTs on `(store_name, key, version)` so replay is a no-op.
- Per-partition ordering preserved. With `WORKERS>1` (recommended for fast
  chains) messages are sharded by partition so each partition's writes still
  flow through a single worker; cross-partition writes parallelize.
- Offsets commit only after the sink persists the entry.
- Sink writes use bounded exponential backoff (5 attempts, 1s→30s) before
  giving up. On give-up the process exits non-zero so the supervisor restarts;
  Kafka offsets stay uncommitted, so the next run replays from the last commit.

## Read-side optimization tables

Two optional tables make trace-style reads dramatically faster. Both are off
by default; flip on by setting the matching env var before `deploy.sh`:

- `state_latest` — one row per `(store, key)` with the most recent value.
  Reads at "current state" become a single PK lookup instead of a descending
  scan on `state_mutations`. Enable with `ENABLE_LATEST=true`. ~2× the write
  rate; cheap on Cockroach.
- `state_at_block` — dense end-of-block snapshot for hot stores. Each block
  copies state_latest into state_at_block, so reads at any block in the
  rolling window are a single PK lookup per `(store, key)`. Set
  `SNAPSHOT_STORES="slashing,distribution,staking,bank,params"` (requires
  `ENABLE_LATEST=true`); bound storage with `SNAPSHOT_WINDOW_BLOCKS=2000` so
  the consumer GCs older blocks inline.

The sentinel-pointer pattern was net-negative on pebble (compaction lag);
moved here it's net-positive because the write tax goes to a system designed
to absorb it.
