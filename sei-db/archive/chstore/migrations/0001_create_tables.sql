-- The archive's tables.
--
-- Block-ordered tables are partitioned by 10M-block ranges, and hash-keyed tables into 32 buckets
-- so a lookup reads one bucket's parts. Every table keeps the deduplication log of its last
-- 100,000 inserts, so a retried insert is written once, and has automatic statistics off: they
-- cost CPU on every insert and merge.
--
-- Account and storage state is kept twice over: *_changes holds each block's changes, ordered by
-- block, and *_history holds every block in which each account or slot changed, ordered by key.
-- A read of a slot at block H takes the newest block <= H from storage_history, then that block's
-- row from storage_changes.
--
-- A lookup in a hash-keyed table (hash_locator, account_history, storage_history) reads one
-- granule from every part in its bucket. Background merges stop at 150 GB per part, so a bucket
-- gains about one part per 150 GB it grows. If those lookups get slow, raising the cap for that
-- table cuts its part count, at the cost of longer merges and free disk for the largest one.
-- Untested at production size:
--   ALTER TABLE hash_locator MODIFY SETTING max_bytes_to_merge_at_max_space_in_pool = 1099511627776;

-- One row per committed batch: every block from start_block to end_block is in every table.
CREATE TABLE IF NOT EXISTS archive_commits
(
    start_block             UInt64,
    end_block               UInt64,
    committed_at            DateTime64(3, 'UTC')
)
ENGINE = MergeTree
ORDER BY end_block
SETTINGS non_replicated_deduplication_window = 100000, auto_statistics_types = '';

CREATE TABLE IF NOT EXISTS blocks
(
    block_number            UInt64 CODEC(Delta, ZSTD(1)),
    block_hash              FixedString(32) CODEC(NONE),
    parent_hash             FixedString(32) CODEC(NONE),
    block_time              DateTime('UTC') CODEC(Delta, ZSTD(1)),
    transaction_count       UInt32 CODEC(ZSTD(1)),
    gas_used                UInt64 CODEC(ZSTD(1)),
    gas_limit               UInt64 CODEC(ZSTD(1)),
    base_fee_per_gas        UInt256 CODEC(ZSTD(1))
)
ENGINE = MergeTree
PARTITION BY intDiv(block_number, 10000000)
ORDER BY block_number
SETTINGS index_granularity = 1024, non_replicated_deduplication_window = 100000, auto_statistics_types = '';

-- executed is 0 for a transaction that is in its block but never ran.
CREATE TABLE IF NOT EXISTS transactions_receipts
(
    block_number            UInt64 CODEC(Delta, ZSTD(1)),
    transaction_index       UInt32 CODEC(Delta, ZSTD(1)),
    transaction_hash        FixedString(32) CODEC(NONE),
    sender                  FixedString(20) CODEC(ZSTD(1)),
    transaction_type        UInt8,
    transaction_payload     String CODEC(ZSTD(1)),
    receipt_status          UInt8,
    cumulative_gas_used     UInt64 CODEC(Delta, ZSTD(1)),
    gas_used                UInt64 CODEC(ZSTD(1)),
    effective_gas_price     UInt256 CODEC(ZSTD(1)),
    has_contract_address    UInt8,
    contract_address        FixedString(20) CODEC(ZSTD(1)),
    executed                UInt8
)
ENGINE = MergeTree
PARTITION BY intDiv(block_number, 10000000)
ORDER BY (block_number, transaction_index)
SETTINGS non_replicated_deduplication_window = 100000, auto_statistics_types = '';

-- Finds a block or transaction by hash: hash_prefix is the hash's big-endian first eight bytes,
-- and location the block number, or the block number << 24 | transaction index. A hash with
-- several locations resolves to the earliest. The prefixes are random but sorted, so their deltas
-- are small.
CREATE TABLE IF NOT EXISTS hash_locator
(
    hash_prefix             UInt64 CODEC(Delta, ZSTD(1)),
    object_kind             UInt8,
    location                UInt64 CODEC(ZSTD(1))
)
ENGINE = MergeTree
PARTITION BY modulo(hash_prefix, 32)
ORDER BY (hash_prefix, object_kind, location)
SETTINGS index_granularity = 4096, non_replicated_deduplication_window = 100000, auto_statistics_types = '';

-- The text index holds, per log, 'a' plus the raw address and the topic position digit
-- ('0'..'3') plus each raw topic.
CREATE TABLE IF NOT EXISTS logs
(
    block_number            UInt64 CODEC(Delta, ZSTD(1)),
    transaction_index       UInt32 CODEC(Delta, ZSTD(1)),
    log_index               UInt32 CODEC(Delta, ZSTD(1)),
    block_hash              FixedString(32) CODEC(ZSTD(1)),
    transaction_hash        FixedString(32) CODEC(ZSTD(1)),
    address                 FixedString(20) CODEC(ZSTD(1)),
    topics                  Array(FixedString(32)) CODEC(ZSTD(1)),
    data                    String CODEC(ZSTD(1)),
    INDEX getlogs_idx arrayConcat([concat('a', toString(address))],
        arrayMap((t, i) -> concat(char(47 + i), toString(t)), topics, arrayEnumerate(topics)))
        TYPE text(tokenizer = array)
)
ENGINE = MergeTree
PARTITION BY intDiv(block_number, 10000000)
ORDER BY (block_number, transaction_index, log_index)
SETTINGS index_granularity = 2048, non_replicated_deduplication_window = 100000, auto_statistics_types = '';

-- The state of each account a block changed, after the block.
CREATE TABLE IF NOT EXISTS account_changes
(
    block_number            UInt64 CODEC(Delta, ZSTD(1)),
    address                 FixedString(20) CODEC(ZSTD(3)),
    nonce                   UInt64 CODEC(ZSTD(1)),
    balance                 UInt256 CODEC(ZSTD(3)),
    code_hash               FixedString(32) CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY intDiv(block_number, 10000000)
ORDER BY (block_number, address)
SETTINGS non_replicated_deduplication_window = 100000, auto_statistics_types = '';

-- The blocks in which each account changed. block_number keeps per-part min/max statistics, so a
-- read at an old height skips the parts written after it.
CREATE TABLE IF NOT EXISTS account_history
(
    address                 FixedString(20) CODEC(ZSTD(3)),
    block_number            UInt64 CODEC(Delta, ZSTD(1)) STATISTICS(basic)
)
ENGINE = MergeTree
PARTITION BY modulo(sipHash64(address), 32)
ORDER BY (sipHash64(address), block_number)
SETTINGS non_replicated_deduplication_window = 100000, auto_statistics_types = '';

-- The value of each slot a block wrote, after the block; a cleared slot is zero.
CREATE TABLE IF NOT EXISTS storage_changes
(
    block_number            UInt64 CODEC(Delta, ZSTD(1)),
    address                 FixedString(20) CODEC(ZSTD(3)),
    slot                    FixedString(32) CODEC(ZSTD(3)),
    value                   FixedString(32) CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY intDiv(block_number, 10000000)
ORDER BY (block_number, address, slot)
SETTINGS non_replicated_deduplication_window = 100000, auto_statistics_types = '';

-- The blocks in which each slot changed, with min/max statistics on block_number as in
-- account_history.
CREATE TABLE IF NOT EXISTS storage_history
(
    address                 FixedString(20) CODEC(ZSTD(3)),
    slot                    FixedString(32) CODEC(ZSTD(3)),
    block_number            UInt64 CODEC(Delta, ZSTD(1)) STATISTICS(basic)
)
ENGINE = MergeTree
PARTITION BY modulo(sipHash64(address, slot), 32)
ORDER BY (sipHash64(address, slot), block_number)
SETTINGS non_replicated_deduplication_window = 100000, auto_statistics_types = '';

-- Bytecode deployed more than once is written more than once; merges keep one copy.
CREATE TABLE IF NOT EXISTS contract_code
(
    code_hash               FixedString(32) CODEC(NONE),
    code                    String CODEC(ZSTD(3))
)
ENGINE = ReplacingMergeTree
ORDER BY code_hash
SETTINGS index_granularity = 2048, non_replicated_deduplication_window = 100000, auto_statistics_types = '';
