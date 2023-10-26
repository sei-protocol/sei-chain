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

# AsyncFlush defines if committing the block should also wait for the data to be persisted in the StateStore.
# If true, data will be written to StateStore in a async manner to reduce latency.
# default to true
async-flush = {{ .StateStore.AsyncFlush }}

`
