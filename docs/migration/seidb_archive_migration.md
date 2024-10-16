# SeiDB Archive Migration Guide

## Overview
SeiDB is the next generation of chain storage in SeiV2.
One issue for running SeiDB on archive nodes is that we need to keep the full state of the chain, so we can't
state sync it and clear out previous iavl data.

In order to run an archive node with SeiDB, we need to run a migration from iavl to sei-db.

The overall process will work as follows:

1. Stop archive node and note down its height, call it MIGRATION_HEIGHT
2. Update config to enable SeiDB (state committment + state store)
3. Run sc migration at the MIGRATION_HEIGHT
4. Re start seid with `--migrate-iavl` enabled (migrating state store in background)
5. Verify migration at various sampled heights once state store is complete
6. Stop seid, clear out iavl and restart seid normally, now only using SeiDB fully

You may need to ensure you have sufficient disk space available, as during the migration process, both IAVL and SeiDB state stores will need to be maintained simultaneously. This could potentially double your storage requirements temporarily.


## Migration Steps

### Step 1: Stop Node and note down latest height
Stop the seid process and note down the latest height. Save it as an env var $MIGRATION_HEIGHT.
```bash
MIGRATION_HEIGHT=$(seid q block | jq ".block.last_commit.height" | tr -d '"')
systemctl stop seid
```

### Step 2: Add SeiDB Configurations
We can enable SeiDB by adding the following configs to app.toml file.
Usually you can find this file under ~/.sei/config/app.toml.
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
# In order to use state-store, you need to make sure to enable state-commit at the same time.
# Validator nodes should turn this off.
# State sync node or full nodes should turn this on.
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
# Setting it to 0 means keep everything, default to 100000
ss-keep-recent = 0

# PruneIntervalSeconds defines the minimum interval in seconds + some random delay to trigger pruning.
# It is more efficient to trigger pruning less frequently with large interval.
# default to 600 seconds
ss-prune-interval = 600

# ImportNumWorkers defines the concurrency for state sync import
# defaults to 1
ss-import-num-workers = 1
```


### Step 3: Run SC Migration

```bash
seid tools migrate-iavl --target-db SC --home-dir /root/.sei
```

This may take a couple hours to run. You will see logs of form
`Start restoring SC store for height`


### Step 4: Restart seid with background SS migration
```bash
seid start --migrate-iavl --migrate-height $MIGRATION_HEIGHT --chain-id pacific-1
```

Seid will run normally and the migration will run in the background. Data from iavl
will be written to SS and new writes will be directed at SS not iavl.

You will see logs of form 
`SeiDB Archive Migration: Iterating through %s module...` and 
`SeiDB Archive Migration: Last 1,000,000 iterations took:...`


NOTE: While this is running, any historical queries will be routed to iavl if
they are for a height BEFORE the migrate-height. Any queries on heights
AFTER the migrate-height will be routed to state store (pebbbledb).


### Step 5: Verify State Store Migration after completion
Once State Store Migration is complete, you will see logs of form
`SeiDB Archive Migration: DB scanning completed. Total time taken:...`

You DO NOT immediately need to do anything. Your node will continue to run
and will operate normally. However we added a verification tool that will iterate through
all keys in iavl at a specific height and verify they exist in State Store.

You should run the following command for a selection of different heights
```bash
seid tools verify-migration --version $VERIFICATION_HEIGHT
```

This will output `Verification Succeeded` if the verification was successful.


### Step 6: Clear out Iavl and restart seid
Once the verification is complete, we can proceed to clear out the iavl and
restart seid normally.

```bash
systemctl stop seid
rm -rf ~/.sei/data/application.db
seid start --chain-id pacific-1
```


## FAQ
