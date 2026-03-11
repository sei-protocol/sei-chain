package config

// StateCommitConfigTemplate defines the configuration template for state-commit
const StateCommitConfigTemplate = `
###############################################################################
###                       State Commit Configuration                        ###
###############################################################################

[state-commit]
# Enable defines if the SeiDB should be enabled to override existing IAVL db backend.
sc-enable = {{ .StateCommit.Enable }}

# Defines the SC store directory, if not explicitly set, default to application home directory
sc-directory = "{{ .StateCommit.Directory }}"

# WriteMode defines how EVM data writes are routed between backends.
# Valid values: cosmos_only, dual_write, split_write, evm_only
# defaults to cosmos_only
sc-write-mode = "{{ .StateCommit.WriteMode }}"

# ReadMode defines how EVM data reads are routed between backends.
# Valid values: cosmos_only, evm_first, split_read
# defaults to cosmos_only
sc-read-mode = "{{ .StateCommit.ReadMode }}"

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
`

// DefaultConfigTemplate combines both templates for backward compatibility
const DefaultConfigTemplate = StateCommitConfigTemplate + StateStoreConfigTemplate + ReceiptStoreConfigTemplate
