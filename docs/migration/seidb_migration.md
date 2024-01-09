# SeiDB Migration Guide

## Overview
SeiDB is the next generation of chain storage in SeiV2.
This document covers the details of how to migrate validator node and full node from the old IAVL based storage to SeiDB.

By default, SeiDB is disabled and will fallback to IAVL storage, which means once you upgrade to v3.6.0 or later versions,
your nodes can still run with the same old storage as before without performing this migration.

## Prerequisite
- SeiDB requires go 1.19+, if you are still on go 1.18 or below, please update your golang version to 1.19+
- Upgrade Sei-Chain to v3.6.0 or higher versions is required
- Migrating to SeiDB requires a full state sync which would wipe out all your existing data

## SeiDB Introduction
SeiDB is designed to replace the current IAVL based storage in cosmos SDK,
which aims to improve the overall state access performance and tackle any potential state bloat issues.

### Problems SeiDB Solve
- Performance Degradation
	- Node performance degrades a lot as the underline DB size grows larger and larger
	- Constant state sync is needed to prevent the node from keep falling behind
	- Pruning is too slow and not able to keep up when data is huge
- State Bloat
	- Disk size grows really fast and tend to fill up the disk quickly
	- Archive node becomes unmanageable, not able to keep up with the latest block
- Slow state sync (export & import)
	- Exporting or importing a state snapshot could take hours to complete when state grows large
- Slow catching up (block sync) time
	- Block sync is not fast enough, could take hours to catch up to the latest height

### Benefits Migrating to SeiDB
- Disk size growth rate reduced by 90%
- Avoid performance degradation and the need to perform constant state sync
- Commit becomes async, commit latency improved by 200x
- Faster state sync, overall state sync speed improved by at least 10x
- Faster block sync, catching up speed improved by 2x

## Migration Steps

### Step 1: Add Configurations
To enable SeiDB, you need to add the following configs to app.toml file:
```bash
#############################################################################
###                             SeiDB Configuration                       ###
#############################################################################

[state-commit]
# Enable defines if the SeiDB should be enabled to override existing IAVL db backend.
sc-enable = true

# AsyncCommitBuffer defines the size of asynchronous commit queue, this greatly improve block catching-up
# performance, <=0 means synchronous commit.
sc-async-commit-buffer = 100

# SnapshotKeepRecent defines how many memiavl snapshots (beyond the latest one) to keep
# Recommend to set to 1 to make sure IBC relayers work.
sc-keep-recent = 1

# SnapshotInterval defines the number of blocks interval the memiavl snapshot is taken, default to 10000 blocks.
# Adjust based on your needs:
# Setting this too low could lead to lot of extra heavy disk IO
# Setting this too high could lead to slow restart
sc-snapshot-interval = 10000

# SnapshotWriterLimit defines the max concurrency for taking memiavl snapshot
sc-snapshot-writer-limit = 1

# CacheSize defines the size of the LRU cache for each store on top of the tree, default to 100000.
sc-cache-size = 100000

[state-store]
# Enable defines if the state-store should be enabled for historical queries.
# In order to use state-store, you need to make sure to enable state-commit at the same time
# Validator nodes should turn this off
# State sync or full nodes should turn this on
ss-enable = true

# DBBackend defines the backend database used for state-store.
# Supported backends: pebbledb, rocksdb
# defaults to pebbledb (recommended)
ss-backend = "pebbledb"

# AsyncWriteBuffer defines the async queue length for commits to be applied to State Store
# Set <= 0 for synchronous writes, which means commits also need to wait for data to be persisted in State Store.
# defaults to 100
ss-async-write-buffer = 100

# KeepRecent defines the number of versions to keep in state store
# Setting it to 0 means keep everything, default to 0
ss-keep-recent = 10000

# PruneIntervalSeconds defines the minimum interval in seconds + some random delay to trigger pruning.
# It is more efficient to trigger pruning less frequently with large interval.
# default to 600 seconds
ss-prune-interval = 60

# ImportNumWorkers defines the concurrency for state sync import
# defaults to 1
ss-import-num-workers = 1
```

### Step 2: Config Tuning

### Step 3: State Sync

## Rollback Steps


## FAQ

### Where can I find the data after migrating to SeiDB


### Does SeiDB support archive node?
SeiDB support archive node, however there's currently no easy migration process for archive node,
so you can not convert any existing archive node to SeiDB yet.

However, archive node does get much better performance and storage efficiency if running on SeiDB.
If you want run archive node on top of SeiDB, for now, it is recommended to start running a new node with SeiDB.


