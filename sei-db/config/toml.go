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

# ZeroCopy defines if memiavl should return slices pointing to mmap-ed buffers directly (zero-copy),
# the zero-copied slices must not be retained beyond current block's execution.
# the sdk address cache will be disabled if zero-copy is enabled.
sc-zero-copy = {{ .StateCommit.ZeroCopy }}

# Max concurrent historical proof queries (RPC /store path)
sc-historical-proof-max-inflight = {{ .StateCommit.HistoricalProofMaxInFlight }}

# Historical proof query rate limit in req/sec (<=0 disables rate limiting)
sc-historical-proof-rate-limit = {{ .StateCommit.HistoricalProofRateLimit }}

# Historical proof query burst size
sc-historical-proof-burst = {{ .StateCommit.HistoricalProofBurst }}

# AsyncCommitBuffer defines the size of asynchronous commit queue, this greatly improve block catching-up
# performance, setting to 0 means synchronous commit.
sc-async-commit-buffer = {{ .StateCommit.AsyncCommitBuffer }}

# KeepRecent defines how many state-commit snapshots (besides the latest one) to keep
# defaults to 0 to only keep one current snapshot
sc-keep-recent = {{ .StateCommit.SnapshotKeepRecent }}

# SnapshotInterval defines the block interval the snapshot is taken, default to 10000 blocks.
sc-snapshot-interval = {{ .StateCommit.SnapshotInterval }}

# SnapshotMinTimeInterval defines the minimum time interval (in seconds) between snapshots.
# This prevents excessive snapshot creation during catch-up and ensures snapshots don't overlap
# (current snapshot creation takes 3+ hours). Default to 3600 seconds (1 hour).
# Note: If you set a small sc-snapshot-interval (e.g., < 5000), you may want to reduce this value
# to allow more frequent snapshots during normal operation.
sc-snapshot-min-time-interval = {{ .StateCommit.SnapshotMinTimeInterval }}

# SnapshotPrefetchThreshold defines the page cache residency threshold (0.0-1.0) to trigger snapshot prefetch.
# Prefetch sequentially reads nodes/leaves files into page cache for faster cold-start replay.
# Only active trees (evm/bank/acc/wasm) are prefetched, skipping sparse kv files to save memory.
# Skips prefetch if more than threshold of pages already resident (e.g., 0.8 = 80%).
# Defaults to 0.8
sc-snapshot-prefetch-threshold = {{ .StateCommit.SnapshotPrefetchThreshold }}

# Maximum snapshot write rate in MB/s (global across all trees). 0 = unlimited. Default 100.
sc-snapshot-write-rate-mbps = {{ .StateCommit.SnapshotWriteRateMBps }}

# OnlyAllowExportOnSnapshotVersion defines whether we only allow state sync
# snapshot creation happens after the memiavl snapshot is created.
sc-only-allow-export-on-snapshot-version = {{ .StateCommit.OnlyAllowExportOnSnapshotVersion }}
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

# HashRange defines the range of blocks after which a XOR hash is computed and stored
# defaults to 1,000,000 blocks. Set to -1 to disable.
ss-hash-range = {{ .StateStore.HashRange }}
`

// DefaultConfigTemplate combines both templates for backward compatibility
const DefaultConfigTemplate = StateCommitConfigTemplate + StateStoreConfigTemplate
