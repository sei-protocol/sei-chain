package config

// StateCommitConfigTemplate defines the configuration template for state-commit
const StateCommitConfigTemplate = `
###############################################################################
###                       State Commit Configuration                        ###
###############################################################################

[state-commit]
# Enable defines if the SeiDB state-commit should be enabled.
sc-enable = {{ .StateCommit.Enable }}

# Defines the SC store directory, if not explicitly set, default to application home directory
sc-directory = "{{ .StateCommit.Directory }}"

# Max concurrent historical proof queries (RPC /store path)
sc-historical-proof-max-inflight = {{ .StateCommit.HistoricalProofMaxInFlight }}

# Historical proof query rate limit in req/sec (<=0 disables rate limiting)
sc-historical-proof-rate-limit = {{ .StateCommit.HistoricalProofRateLimit }}

# Historical proof query burst size
sc-historical-proof-burst = {{ .StateCommit.HistoricalProofBurst }}

# AsyncCommitBuffer defines the size of asynchronous commit queue, this greatly improve block catching-up
# performance, setting to 0 means synchronous commit.
sc-async-commit-buffer = {{ .StateCommit.MemIAVLConfig.AsyncCommitBuffer }}

# KeepRecent defines how many state-commit snapshots (besides the latest one) to keep
# defaults to 0 to only keep one current snapshot
sc-keep-recent = {{ .StateCommit.MemIAVLConfig.SnapshotKeepRecent }}

# SnapshotInterval defines the block interval the snapshot is taken, default to 10000 blocks.
sc-snapshot-interval = {{ .StateCommit.MemIAVLConfig.SnapshotInterval }}

# SnapshotMinTimeInterval defines the minimum time interval (in seconds) between snapshots.
# This prevents excessive snapshot creation during catch-up and ensures snapshots don't overlap
# (current snapshot creation takes 3+ hours). Default to 3600 seconds (1 hour).
# Note: If you set a small sc-snapshot-interval (e.g., < 5000), you may want to reduce this value
# to allow more frequent snapshots during normal operation.
sc-snapshot-min-time-interval = {{ .StateCommit.MemIAVLConfig.SnapshotMinTimeInterval }}

# SnapshotPrefetchThreshold defines the page cache residency threshold (0.0-1.0) to trigger snapshot prefetch.
# Prefetch sequentially reads nodes/leaves files into page cache for faster cold-start replay.
# Only active trees (evm/bank/acc/wasm) are prefetched, skipping sparse kv files to save memory.
# Skips prefetch if more than threshold of pages already resident (e.g., 0.8 = 80%).
# Defaults to 0.8
sc-snapshot-prefetch-threshold = {{ .StateCommit.MemIAVLConfig.SnapshotPrefetchThreshold }}

# Maximum snapshot write rate in MB/s (global across all trees). 0 = unlimited. Default 100.
sc-snapshot-write-rate-mbps = {{ .StateCommit.MemIAVLConfig.SnapshotWriteRateMBps }}

# WriteMode defines the write routing mode for EVM data in the SC layer.
# Valid values: memiavl_only, migrate_evm, evm_migrated, migrate_all_but_bank,
# all_migrated_but_bank, migrate_bank, flatkv_only, test_only_dual_write
sc-write-mode = "{{ .StateCommit.WriteMode }}"

# KeysToMigratePerBlock controls how many EVM keys the in-flight migration
# (sc-write-mode = migrate_evm / migrate_bank / migrate_all_but_bank) drains
# from memiavl into flatkv per block. Default 1024 is appropriate for
# production drains; lower it (e.g. 256) to spread the migration across more
# blocks for test runs that need to observe the resume / hybrid-read path.
# Must be > 0; ignored entirely when not in a migration mode.
sc-keys-to-migrate-per-block = {{ .StateCommit.KeysToMigratePerBlock }}

# MigrateEVMStartHeight, when > 0 and sc-write-mode = migrate_evm, defers the
# start of the EVM migration until the chain reaches this block height. Before
# it the node behaves like memiavl_only for the EVM module (EVM reads/writes
# stay on memiavl, the migration boundary stays NotStarted, and the flatkv
# lattice is excluded from the AppHash), so it stays AppHash-compatible with
# memiavl-only peers; at this height the per-block drain begins.
# 0 (default) starts the migration immediately when migrate_evm is configured.
sc-migrate-evm-start-height = {{ .StateCommit.MigrateEVMStartHeight }}

# HashLogger records a per-block CSV of named hashes (memIAVL module/root hashes, flatKV DB/root
# hashes, the app hash, the block hash, and the changeset hash) so block-hash computation can be
# studied and compared across nodes. It is a debugging/forensics tool; enabled by default.
sc-hash-logger-enable = {{ .StateCommit.HashLogger.Enable }}

# Directory for hash log files. If empty, defaults to a "hash.log" directory under the SC store's data
# directory (i.e. <home>/data/hash.log).
sc-hash-logger-directory = "{{ .StateCommit.HashLogger.Directory }}"

# Number of most-recent blocks to retain on disk. 0 disables block-count retention (disk-size cap only).
sc-hash-logger-blocks-to-retain = {{ .StateCommit.HashLogger.BlocksToRetain }}

# Size in bytes a hash log file may reach before it is sealed and rotated. Must be > 0.
sc-hash-logger-target-file-size = {{ .StateCommit.HashLogger.TargetFileSize }}

# Backstop cap in bytes on the total size of sealed hash log files. 0 disables the disk-size cap
# (block-count retention only).
sc-hash-logger-max-disk-size = {{ .StateCommit.HashLogger.MaxDiskSize }}

###############################################################################
###                        FlatKV (EVM) Configuration                       ###
###############################################################################

[state-commit.flatkv]
# Fsync controls whether PebbleDB writes (data DBs + metadataDB) use fsync.
# WAL always uses NoSync (matching memiavl); crash recovery relies on
# WAL catchup, which is idempotent. Default: false.
fsync = {{ .StateCommit.FlatKVConfig.Fsync }}

# AsyncWriteBuffer defines the size of the async write buffer for data DBs.
# Set <= 0 for synchronous writes.
async-write-buffer = {{ .StateCommit.FlatKVConfig.AsyncWriteBuffer }}

# SnapshotInterval defines how often (in blocks) a PebbleDB checkpoint is taken.
# 0 disables auto-snapshots. Default: 10000.
snapshot-interval = {{ .StateCommit.FlatKVConfig.SnapshotInterval }}

# SnapshotKeepRecent defines how many old snapshots to keep besides the latest one.
# 0 = keep only the current snapshot. Default: 2.
snapshot-keep-recent = {{ .StateCommit.FlatKVConfig.SnapshotKeepRecent }}
`

// StateStoreConfigTemplate defines the configuration template for state-store
const StateStoreConfigTemplate = `
###############################################################################
###                         State Store Configuration                       ###
###############################################################################

[state-store]
# Enable defines whether the state-store should be enabled for storing historical data.
# Supporting historical queries or exporting state snapshot requires setting this to true
# This config only take effect when SeiDB is enabled (sc-enable = true)
ss-enable = {{ .StateStore.Enable }}

# Defines the directory to store the state store db files
# If not explicitly set, default to application home directory
ss-db-directory = "{{ .StateStore.DBDirectory }}"

# DBBackend defines the backend database used for state-store.
# Supported backends: pebbledb, rocksdb
# defaults to pebbledb (recommended)
ss-backend = "{{ .StateStore.Backend }}"

# AsyncWriteBuffer defines the async queue length for commits to be applied to State Store
# Set <= 0 for synchronous writes, which means commits also need to wait for data to be persisted in State Store.
# defaults to 100 for asynchronous writes
ss-async-write-buffer = {{ .StateStore.AsyncWriteBuffer }}

# KeepRecent defines the number of versions to keep in state store
# Setting it to 0 means keep everything
# Default to keep the last 100,000 blocks
ss-keep-recent = {{ .StateStore.KeepRecent }}

# PruneInterval defines the minimum interval in seconds + some random delay to trigger SS pruning.
# It is recommended to trigger pruning less frequently with a large interval.
# default to 600 seconds
ss-prune-interval = {{ .StateStore.PruneIntervalSeconds }}

# ImportNumWorkers defines the concurrency for state sync import
# defaults to 1
ss-import-num-workers = {{ .StateStore.ImportNumWorkers }}

# EVMDBDirectory defines the directory for the optional EVM state-store DB(s).
# If unset, defaults to <home>/data/evm_ss when EVM SS is enabled.
evm-ss-db-directory = "{{ .StateStore.EVMDBDirectory }}"

# EVMSplit controls whether EVM data is routed to a dedicated SS backend.
# When false (default), EVM data lives in the Cosmos SS backend alongside
# everything else. When true, EVM data is routed exclusively to the EVM SS
# backend; non-EVM data stays in Cosmos SS. No fallback between backends.
evm-ss-split = {{ .StateStore.EVMSplit }}

# SeparateEVMSubDBs controls whether EVM data is split across per-type DBs.
# When false, all EVM data stays in one DB using the current unified layout.
# When true, data is routed to separate DBs while preserving the same evm key prefix format.
evm-ss-separate-dbs = {{ .StateStore.SeparateEVMSubDBs }}
`

// ReceiptStoreConfigTemplate defines the configuration template for receipt-store
const ReceiptStoreConfigTemplate = `
###############################################################################
###                        Receipt Store Configuration                      ###
###############################################################################

[receipt-store]
# Backend defines the receipt store backend.
# Supported backends: pebble (aka pebbledb), parquet
# defaults to pebbledb
rs-backend = "{{ .ReceiptStore.Backend }}"

# Defines the receipt store directory. If unset, defaults to <home>/data/ledger/receipt/{backend}
db-directory = "{{ .ReceiptStore.DBDirectory }}"

# AsyncWriteBuffer defines the async queue length for commits to be applied to receipt store.
# Applies only when rs-backend = "pebbledb"; parquet ignores this setting.
# Set <= 0 for synchronous writes.
# defaults to 100
async-write-buffer = {{ .ReceiptStore.AsyncWriteBuffer }}

# PruneIntervalSeconds defines the interval in seconds to trigger pruning.
# Receipt retention is controlled by the global min-retain-blocks flag.
# defaults to 600 seconds
prune-interval-seconds = {{ .ReceiptStore.PruneIntervalSeconds }}

# TxIndexBackend selects the tx-hash index implementation for parquet receipts.
# Set to "pebbledb" to enable the index, or "" to disable it.
# Ignored unless rs-backend = "parquet".
tx-index-backend = "{{ .ReceiptStore.TxIndexBackend }}"
`

// DefaultConfigTemplate combines both templates for backward compatibility
const DefaultConfigTemplate = StateCommitConfigTemplate + StateStoreConfigTemplate + ReceiptStoreConfigTemplate
