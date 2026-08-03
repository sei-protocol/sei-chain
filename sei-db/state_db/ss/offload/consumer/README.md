# Historical State Offload (Bigtable)

Bigtable holds immutable MVCC mutation rows for history that local SS has
pruned. The shape is narrow:

- local SS remains the hot store for recent state, writes, imports, pruning, and iterators
- Bigtable keeps immutable MVCC mutation rows for older history
- reads below local SS retention can fall back to Bigtable for `Get` and `Has`

Row keys are salted with an inverted height suffix:

```text
m | shard(store,key) | store_name | state_key | inverted_height
```

Reads scan from `inverted(target_height)` and stop after the first row, giving
the latest write at or before the requested height. Ordered prefix iteration is
intentionally not served from the offload store.

## Consumer

The consumer reads historical offload changelog messages from Kafka and writes
them into Bigtable. Kafka offsets are committed only after the sink write
succeeds. Mutation rows are written before the version marker.

```bash
cbt -project my-gcp-project -instance sei-history createtable state_mutations
cbt -project my-gcp-project -instance sei-history createfamily state_mutations state

go run ./sei-db/state_db/ss/offload/consumer/cmd/historical-offload-consumer \
  ./sei-db/state_db/ss/offload/consumer/config/example-bigtable.json
```

The example config is a local/dev placeholder. Set real Kafka brokers and
Bigtable credentials/config in your own config.

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

## Current Limits

- The node-side read fallback lands in part 2; this part is the client library
  and the ingestion pipeline.
- No cross-row transaction on ingest; mutation rows are written first and the
  version marker is written last, so replay is idempotent after partial failure.
- No automatic table creation from the binary.
- No backfill tooling; coverage starts when ingestion starts.
