-- ScyllaDB/Cassandra schema for Sei historical state offload.
-- Apply once before running historical-offload-consumer.

CREATE KEYSPACE IF NOT EXISTS sei_history
WITH replication = {
  'class': 'SimpleStrategy',
  'replication_factor': '1'
};

-- For production, replace the keyspace replication with
-- NetworkTopologyStrategy and the real datacenter replication factors.

USE sei_history;

-- Version markers are written after all mutation rows for a block version.
-- Buckets avoid a single hot partition while keeping LastVersion bounded to
-- 64 small point reads.
CREATE TABLE IF NOT EXISTS state_versions (
  bucket int,
  version bigint,
  kafka_topic text,
  kafka_partition int,
  kafka_offset bigint,
  ingested_at timestamp,
  PRIMARY KEY ((bucket), version)
) WITH CLUSTERING ORDER BY (version DESC);

-- Historical point lookup:
--   WHERE store_name = ? AND state_key = ? AND version <= ?
--   ORDER BY version DESC LIMIT 1
CREATE TABLE IF NOT EXISTS state_mutations (
  store_name text,
  state_key blob,
  version bigint,
  value blob,
  deleted boolean,
  PRIMARY KEY ((store_name, state_key), version)
) WITH CLUSTERING ORDER BY (version DESC);

CREATE TABLE IF NOT EXISTS state_tree_upgrades (
  version bigint,
  name text,
  rename_from text,
  deleted boolean,
  PRIMARY KEY ((version), name)
);
