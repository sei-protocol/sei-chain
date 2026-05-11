# Historical Scylla/Cassandra Offload

This is a prototype historical-state backend for ScyllaDB or Cassandra.

The intended shape is narrow:

- local SS remains the hot store for recent state, writes, imports, pruning, and iterators
- Scylla/Cassandra stores immutable MVCC mutation rows for older history
- reads below local SS retention can fall back to Scylla/Cassandra for `Get` and `Has`

The table layout is built for point reads by `(store_name, state_key, target_version)`:

```sql
SELECT version, value, deleted
FROM state_mutations
WHERE store_name = ? AND state_key = ? AND version <= ?
ORDER BY version DESC
LIMIT 1;
```

Ordered prefix iteration is intentionally not served from Scylla/Cassandra in this prototype.

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
them into Scylla/Cassandra. Kafka offsets are committed only after the sink
write succeeds.

```bash
go run ./sei-db/state_db/ss/offload/consumer/cmd/historical-scylla-consumer \
  ./sei-db/state_db/ss/offload/consumer/config/example-scylla.json
```

The example config is local-dev only. Set real Kafka brokers, Scylla hosts,
keyspace, datacenter, and credentials in your own config.

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

Fallback activates only for point reads where the requested version is below the
local SS earliest version. Missing rows and tombstones return empty state, same
as local SS.

## Current Limits

- No Scylla/Cassandra iterator path.
- No cross-row transaction on ingest; mutation rows are written first and the
  version marker is written last, so replay is idempotent after partial failure.
- No automatic schema creation from the binary.
