# Historical State Offload

This is a prototype historical-state backend for ScyllaDB/Cassandra and
Bigtable.

The intended shape is narrow:

- local SS remains the hot store for recent state, writes, imports, pruning, and iterators
- the downstream store keeps immutable MVCC mutation rows for older history
- reads below local SS retention can fall back to the downstream store for `Get` and `Has`

The Scylla table layout is built for point reads by
`(store_name, state_key, target_version)`:

```sql
SELECT version, value, deleted
FROM state_mutations
WHERE store_name = ? AND state_key = ? AND version <= ?
ORDER BY version DESC
LIMIT 1;
```

Bigtable uses salted row keys with an inverted height suffix:

```text
m | shard(store,key) | store_name | state_key | inverted_height
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
sink write succeeds. Mutation rows are written with bounded concurrency and the
version marker is written last.

```bash
go run ./sei-db/state_db/ss/offload/consumer/cmd/historical-scylla-consumer \
  ./sei-db/state_db/ss/offload/consumer/config/example-scylla.json
```

For Bigtable:

```bash
cbt -project my-gcp-project -instance sei-history createtable state_mutations
cbt -project my-gcp-project -instance sei-history createfamily state_mutations state

go run ./sei-db/state_db/ss/offload/consumer/cmd/historical-scylla-consumer \
  ./sei-db/state_db/ss/offload/consumer/config/example-bigtable.json
```

The example configs are local/dev placeholders. Set real Kafka brokers and
backend credentials/config in your own config.

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

Or Bigtable:

```toml
[state-store]
historical-offload-bigtable-project-id = "my-gcp-project"
historical-offload-bigtable-instance = "sei-history"
historical-offload-bigtable-table = "state_mutations"
historical-offload-bigtable-family = "state"
historical-offload-bigtable-app-profile = ""
historical-offload-bigtable-shards = 256
```

Fallback activates only for point reads where the requested version is below the
local SS earliest version. Missing rows and tombstones return empty state, same
as local SS.

## Current Limits

- No offload iterator path.
- No cross-row transaction on ingest; mutation rows are written first and the
  version marker is written last, so replay is idempotent after partial failure.
- No automatic schema creation from the binary.
