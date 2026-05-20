# Historical State Offload

This is a prototype historical-state backend for ScyllaDB/Cassandra and
FoundationDB.

The intended shape is narrow:

- local SS remains the hot store for recent state, writes, imports, pruning, and iterators
- the downstream store keeps immutable MVCC mutation rows for older history
- reads below local SS retention can fall back to the downstream store for `Get` and `Has`

The Scylla table layout is built for point reads by `(store_name, state_key, target_version)`:

```sql
SELECT version, value, deleted
FROM state_mutations
WHERE store_name = ? AND state_key = ? AND version <= ?
ORDER BY version DESC
LIMIT 1;
```

FoundationDB uses tuple-like binary keys with a salted key prefix and inverted
height suffix:

```text
prefix | m | shard(store,key) | store_name | state_key | inverted_height
```

Reads scan from `inverted(target_height)` and stop after the first row, giving
the latest write at or before the requested height.

Ordered prefix iteration is intentionally not served from the offload store in
this prototype.

## Schema

Apply the schema once:

```bash
cqlsh 127.0.0.1 9042 -f sei-db/state_db/ss/offload/consumer/schema/scylla.cql
```

For production, edit the keyspace replication in `schema/scylla.cql` to use
`NetworkTopologyStrategy` with the actual datacenter names and replication
factors before applying it.

## Consumer

The consumer reads historical offload changelog messages from Kafka and writes
them into the configured backend. Kafka offsets are committed only after the
sink write succeeds.

```bash
go run ./sei-db/state_db/ss/offload/consumer/cmd/historical-scylla-consumer \
  ./sei-db/state_db/ss/offload/consumer/config/example-scylla.json
```

For FoundationDB, install the FoundationDB client library and build with the
`foundationdb` tag:

```bash
go run -tags foundationdb ./sei-db/state_db/ss/offload/consumer/cmd/historical-scylla-consumer \
  ./sei-db/state_db/ss/offload/consumer/config/example-foundationdb.json
```

The example configs are local-dev only. Set real Kafka brokers and backend
credentials/config in your own config.

## Node Read Fallback

Enable fallback reads in the node config:

```toml
[state-store]
historical-offload-scylla-hosts = "10.0.0.1:9042,10.0.0.2:9042"
historical-offload-scylla-keyspace = "sei_history"
historical-offload-scylla-username = ""
historical-offload-scylla-password = ""
historical-offload-scylla-datacenter = "datacenter1"
historical-offload-scylla-consistency = "local_quorum"
historical-offload-scylla-timeout-ms = 2000
```

Or FoundationDB:

```toml
[state-store]
historical-offload-foundationdb-enabled = true
historical-offload-foundationdb-cluster-file = ""
historical-offload-foundationdb-prefix = "sei_history"
historical-offload-foundationdb-api-version = 730
historical-offload-foundationdb-shards = 256
historical-offload-foundationdb-transaction-timeout-ms = 10000
historical-offload-foundationdb-transaction-retry-limit = 10
historical-offload-foundationdb-transaction-max-retry-delay-ms = 1000
historical-offload-foundationdb-transaction-size-limit-bytes = 9000000
```

Set transaction knobs to `0` to use the same defaults shown above.

Fallback activates only for point reads where the requested version is below the
local SS earliest version. Missing rows and tombstones return empty state, same
as local SS.

## Current Limits

- No offload iterator path.
- Scylla/Cassandra has no cross-row transaction on ingest; mutation rows are
  written first and the version marker is written last, so replay is idempotent
  after partial failure. FoundationDB row writes are pipelined and version
  markers are written after the rows are durable.
- No automatic schema creation from the binary.
