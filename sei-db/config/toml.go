package config

// DefaultConfigTemplate defines the configuration template for the seiDB configuration
const DefaultConfigTemplate = `
###############################################################################
###                      SeiDB Configuration (Auto-managed)                  ###
###############################################################################

# Defines the SC store directory, if not explicitly set, default to application home directory
sc-directory = "{{ .StateCommit.Directory }}"

# ZeroCopy defines if memiavl should return slices pointing to mmap-ed buffers directly (zero-copy),
# the zero-copied slices must not be retained beyond current block's execution.
# the sdk address cache will be disabled if zero-copy is enabled.
sc-zero-copy = {{ .StateCommit.ZeroCopy }}

# AsyncCommitBuffer defines the size of asynchronous commit queue, this greatly improve block catching-up
# performance, setting to 0 means synchronous commit.
sc-async-commit-buffer = {{ .StateCommit.AsyncCommitBuffer }}

# KeepRecent defines how many state-commit snapshots (besides the latest one) to keep
# defaults to 0 to only keep one current snapshot
sc-keep-recent = {{ .StateCommit.SnapshotKeepRecent }}

# SnapshotWriterLimit defines the max concurrency for taking commit store snapshot
sc-snapshot-writer-limit = {{ .StateCommit.SnapshotWriterLimit }}

# CacheSize defines the size of the LRU cache for each store on top of the tree, default to 100000.
sc-cache-size = {{ .StateCommit.CacheSize }}

# OnlyAllowExportOnSnapshotVersion defines whether we only allow state sync
# snapshot creation happens after the memiavl snapshot is created.
sc-only-allow-export-on-snapshot-version = {{ .StateCommit.OnlyAllowExportOnSnapshotVersion }}

# Defines the directory to store the state store db files
# If not explicitly set, default to application home directory
ss-db-directory = "{{ .StateStore.DBDirectory }}"

# AsyncWriteBuffer defines the async queue length for commits to be applied to State Store
# Set <= 0 for synchronous writes, which means commits also need to wait for data to be persisted in State Store.
# defaults to 100 for asynchronous writes
ss-async-write-buffer = {{ .StateStore.AsyncWriteBuffer }}

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
