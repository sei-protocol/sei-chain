-- CockroachDB schema for the historical offload consumer.
-- Applied once per cluster before starting the consumer.

-- Hash-sharded PK: version is monotonic (block height) and would otherwise
-- pin every write to one range at the head of the keyspace.
CREATE TABLE IF NOT EXISTS state_versions (
    version         INT8        NOT NULL,
    kafka_topic     STRING      NOT NULL,
    kafka_partition INT8        NOT NULL,
    kafka_offset    INT8        NOT NULL,
    ingested_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (version) USING HASH WITH (bucket_count = 16)
);

ALTER TABLE state_versions
    ADD COLUMN IF NOT EXISTS kafka_partition INT8 NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS state_mutations (
    store_name STRING NOT NULL,
    key        BYTES  NOT NULL,
    version    INT8   NOT NULL,
    value      BYTES  NULL,
    deleted    BOOL   NOT NULL DEFAULT false,
    PRIMARY KEY (store_name, key, version DESC)
);

DROP INDEX IF EXISTS state_mutations@state_mutations_by_version_idx;
DROP INDEX IF EXISTS state_mutations@state_mutations_by_store_version_idx;

CREATE TABLE IF NOT EXISTS state_tree_upgrades (
    version     INT8   NOT NULL,
    name        STRING NOT NULL,
    rename_from STRING NOT NULL DEFAULT '',
    delete      BOOL   NOT NULL DEFAULT false,
    PRIMARY KEY (version, name)
);
