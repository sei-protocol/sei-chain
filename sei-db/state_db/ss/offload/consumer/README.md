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
sink write succeeds. Mutation rows are written before the version marker.

```bash
go run ./sei-db/state_db/ss/offload/consumer/cmd/historical-offload-consumer \
  ./sei-db/state_db/ss/offload/consumer/config/example-scylla.json
```

For Bigtable:

```bash
cbt -project my-gcp-project -instance sei-history createtable state_mutations
cbt -project my-gcp-project -instance sei-history createfamily state_mutations state

go run ./sei-db/state_db/ss/offload/consumer/cmd/historical-offload-consumer \
  ./sei-db/state_db/ss/offload/consumer/config/example-bigtable.json
```

The example configs are local/dev placeholders. Set real Kafka brokers and
backend credentials/config in your own config.

For Google Cloud Managed Service for Apache Kafka, connect with TLS plus
SASL/PLAIN using service-account credentials:

```json
"Kafka": {
  "Brokers": ["bootstrap.CLUSTER.REGION.managedkafka.PROJECT.cloud.goog:9092"],
  "TLSEnabled": true,
  "SASLMechanism": "plain",
  "Username": "kafka-client@PROJECT.iam.gserviceaccount.com",
  "Password": "<base64-encoded service account key JSON>"
}
```

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
as local SS. Reads ahead of the backend's last ingested version (consumer lag)
return an error rather than empty state.

To open RPC height gates for pruned heights, declare how far back the backend
has full coverage (the version at which ingestion/backfill started):

```toml
[state-store]
historical-offload-earliest-version = 123456789
```

When set (> 0), the node advertises this as its earliest version so height
checks admit heights the fallback can serve; point reads below it stay on the
local store. Leave it 0 until the backend actually covers the target range —
heights the backend never ingested would otherwise read as empty state.

## Operational preconditions

Before enabling the node read fallback in production:

- The backend table/keyspace exists with the same family/shards (Bigtable) or
  schema (Scylla) as the consumer wrote with.
- The consumer has been ingesting continuously; check
  `consumer_kafka_lag` / `bigtable_rows_mutated_total`.
- `historical-offload-earliest-version` is set to the true coverage floor.

## Current Limits

- No offload iterator path; range queries between the coverage floor and the
  local prune horizon see only local data.
- No cross-row transaction on ingest; mutation rows are written first and the
  version marker is written last, so replay is idempotent after partial failure.
- No automatic schema creation from the binary.
- No backfill tooling; coverage starts when ingestion starts.
