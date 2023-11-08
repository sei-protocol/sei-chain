package config

// DefaultConfigTemplate defines the configuration template for the seiDB configuration
const DefaultConfigTemplate = `
###############################################################################
###                             SeiDB Configuration                       ###
###############################################################################

[state-commit]

# Enable defines if the state-commit (memiavl) should be enabled to override existing IAVL db backend.
enable = {{ .StateCommit.Enable }}

# ZeroCopy defines if memiavl should return slices pointing to mmap-ed buffers directly (zero-copy),
# the zero-copied slices must not be retained beyond current block's execution.
# the sdk address cache will be disabled if zero-copy is enabled.
zero-copy = {{ .StateCommit.ZeroCopy }}

# AsyncCommitBuffer defines the size of asynchronous commit queue, this greatly improve block catching-up
# performance, <=0 means synchronous commit.
async-commit-buffer = {{ .StateCommit.AsyncCommitBuffer }}

# SnapshotKeepRecent defines how many state-commit snapshots (besides the latest one) to keep 
# defaults to 1 to make sure ibc relayers work.
snapshot-keep-recent = {{ .StateCommit.SnapshotKeepRecent }}

# SnapshotInterval defines the block interval the SC snapshot is taken, default to 10000.
snapshot-interval = {{ .StateCommit.SnapshotInterval }}

# CacheSize defines the size of the cache for each memiavl store, default to 100000.
cache-size = {{ .StateCommit.CacheSize }}

[state-store]

# Enable defines if the state-store should be enabled for historical queries.
# In order to use state-store, you need to make sure to enable state-commit at the same time
enable = {{ .StateStore.Enable }}

# DBBackend defines the backend database used for state-store.
# Supported backends: pebbledb, rocksdb
# defaults to pebbledb
backend = "{{ .StateStore.Backend }}"

# AsyncWriteBuffer defines the async queue length for commits to be applied to State Store
# Set <= 0 for synchronous writes, which means commits also need to wait for data to be persisted in State Store.
# defaults to 100
async-write-buffer = {{ .StateStore.AsyncWriteBuffer }}

# KeepRecent defines the number of versions to keep in state store
# Setting it to 0 means keep everything, default to 0
keep-recent = {{ .StateStore.KeepRecent }}

# PruneIntervalSeconds defines the minimum interval in seconds + some random delay to trigger pruning.
# It is more efficient to trigger pruning less frequently with large interval.
# default to 60 seconds
prune-interval-seconds = {{ .StateStore.PruneIntervalSeconds }}

`
