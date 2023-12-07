package config

// DefaultConfigTemplate defines the configuration template for the seiDB configuration
const DefaultConfigTemplate = `
#############################################################################
###                             SeiDB Configuration                       ###
#############################################################################

[state-commit]

# Enable defines if the state-commit (memiavl) should be enabled to override existing IAVL db backend.
sc-enable = {{ .StateCommit.Enable }}

# Directory defines the state-commit store directory, if not explicitly set, default to application home directory
sc-directory = {{ .StateCommit.Directory }}

# ZeroCopy defines if memiavl should return slices pointing to mmap-ed buffers directly (zero-copy),
# the zero-copied slices must not be retained beyond current block's execution.
# the sdk address cache will be disabled if zero-copy is enabled.
sc-zero-copy = {{ .StateCommit.ZeroCopy }}

# AsyncCommitBuffer defines the size of asynchronous commit queue, this greatly improve block catching-up
# performance, <=0 means synchronous commit.
sc-async-commit-buffer = {{ .StateCommit.AsyncCommitBuffer }}

# SnapshotKeepRecent defines how many state-commit snapshots (besides the latest one) to keep 
# defaults to 1 to make sure ibc relayers work.
sc-keep-recent = {{ .StateCommit.SnapshotKeepRecent }}

# SnapshotInterval defines the block interval the snapshot is taken, default to 10000 blocks.
sc-snapshot-interval = {{ .StateCommit.SnapshotInterval }}

# SnapshotWriterLimit defines the max concurrency for taking commit store snapshot 
sc-snapshot-writer-limit = {{ .StateCommit.SnapshotWriterLimit }}

# CacheSize defines the size of the LRU cache for each store on top of the tree, default to 100000.
sc-cache-size = {{ .StateCommit.CacheSize }}

[state-store]

# Enable defines if the state-store should be enabled for historical queries.
# In order to use state-store, you need to make sure to enable state-commit at the same time
ss-enable = {{ .StateStore.Enable }}

# Directory defines the directory to store the state store db files
# If not explicitly set, default to application home directory
ss-db-directory = {{ .StateStore.DBDirectory }}

# DBBackend defines the backend database used for state-store.
# Supported backends: pebbledb, rocksdb, sqlite
# defaults to pebbledb (recommended)
ss-backend = "{{ .StateStore.Backend }}"

# AsyncWriteBuffer defines the async queue length for commits to be applied to State Store
# Set <= 0 for synchronous writes, which means commits also need to wait for data to be persisted in State Store.
# defaults to 100
ss-async-write-buffer = {{ .StateStore.AsyncWriteBuffer }}

# KeepRecent defines the number of versions to keep in state store
# Setting it to 0 means keep everything, default to 0
ss-keep-recent = {{ .StateStore.KeepRecent }}

# PruneIntervalSeconds defines the minimum interval in seconds + some random delay to trigger pruning.
# It is more efficient to trigger pruning less frequently with large interval.
# default to 600 seconds
ss-prune-interval = {{ .StateStore.PruneIntervalSeconds }}

# ImportNumWorkers defines the concurrency for state sync import
# defaults to 1
ss-import-num-workers = {{ .StateStore.ImportNumWorkers }}

`
