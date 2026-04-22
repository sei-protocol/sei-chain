-- CockroachDB schema for the historical offload consumer.
-- Applied once per cluster before starting the consumer.

CREATE TABLE IF NOT EXISTS state_versions (
    version     INT8        PRIMARY KEY,
    kafka_topic STRING      NOT NULL,
    kafka_offset INT8       NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS state_mutations (
    store_name STRING NOT NULL,
    key        BYTES  NOT NULL,
    version    INT8   NOT NULL,
    value      BYTES  NULL,
    deleted    BOOL   NOT NULL DEFAULT false,
    PRIMARY KEY (store_name, key, version DESC)
);

CREATE INDEX IF NOT EXISTS state_mutations_by_version_idx
    ON state_mutations (version);

CREATE TABLE IF NOT EXISTS state_tree_upgrades (
    version     INT8   NOT NULL,
    name        STRING NOT NULL,
    rename_from STRING NOT NULL DEFAULT '',
    delete      BOOL   NOT NULL DEFAULT false,
    PRIMARY KEY (version, name)
);
