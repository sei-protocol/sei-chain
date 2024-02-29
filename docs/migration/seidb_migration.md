# SeiDB Migration Guide

## Overview
SeiDB is the next generation of chain storage in SeiV2.
This document covers the details of how to migrate validator node and full node from the old IAVL based storage to SeiDB.

By default, SeiDB is disabled and will fallback to IAVL storage, which means once you upgrade to v3.6.0 or later versions,
your nodes can still run with the same old storage as before without performing this migration.

## Prerequisite
- Please update your golang version to 1.19+
- Sei-Chain v3.6.0 or higher versions is required
- Migrating to SeiDB requires a full state sync which would wipe out all your existing data

## Hardware Recommendation
Minimum Specs
- CPU: 4 cores
- Memory: 4GB
- Disk: 1000 IOPs & 100MBps
- Network: 1Gbps

Recommended Specs:
- CPU: 8 cores or above
- Memory: 32GB or above
- Disk: 3000 IOPs & 250MBps or above
- Network: 10Gbps

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
- Slow catching up (block sync) speed
	- Block sync is not fast enough, could take hours to catch up to the latest height

### Benefits migrating to SeiDB
- Disk size growth rate reduced by 90%
- Avoid performance degradation over time and the need to perform frequent state sync
- Commits becomes fully async, commit latency improved by 200x
- Faster state sync, overall state sync speed improved by at least 10x after migration
- Faster block sync, catching up speed improved by 2x after migration

## Migration Steps

### Step 1: Add Configurations
To enable SeiDB, you need to add the following configs to app.toml file.
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
ss-keep-recent = 100000

# PruneIntervalSeconds defines the minimum interval in seconds + some random delay to trigger pruning.
# It is more efficient to trigger pruning less frequently with large interval.
# default to 600 seconds
ss-prune-interval = 600

# ImportNumWorkers defines the concurrency for state sync import
# defaults to 1
ss-import-num-workers = 1
```

### Step 2: Tune configs based on node role
If you are migrating a Validator Node:
- Set `sc-enable = true`
- Set `ss-enable = false`

If you are migrating a Full Node:
- Set `sc-enable = true`
- Set `ss-enable = true`
- Set `ss-keep-recent` based on your needs

Note:
Once SeiDB is enabled, the original pruning configs will be ignored, such as
```bash
# The following configs will be ignored and won't take effect if SeiDB is enabled
pruning = "custom"
pruning-keep-recent = "10000"
pruning-keep-every = "0"
pruning-interval = "1000"
```

`PebbleDB` will be used as the default and recommended backend database for full node.

For RocksDB, follow these instructions to first install:
```bash
git clone https://github.com/facebook/rocksdb.git
cd rocksdb

DEBUG_LEVEL=0 make shared_lib install-shared

export LD_LIBRARY_PATH=/usr/local/lib
```
If you run into any issues during installation, please reference [this guide](https://github.com/facebook/rocksdb/blob/master/INSTALL.md).

Once that is complete, you will need to add the following CGO flags:
```bash
CGO_CFLAGS="-I/path/to/rocksdb/include" CGO_LDFLAGS="-L/path/to/rocksdb"
```

and a `rocksdbBackend` tag:

```bash
-tags "rocksdbBackend"
```

to the seid go installation command.

Note: Managing these `rocksdb` CGO dependencies and installation issues is one of the reasons why `pebbledb` (written in pure go) is the default.


### Step 3: State Sync
SeiDB is fully compatible with the existing state snapshot format.
So in order to migrate to use SeiDb, we need to perform a state sync.
Use the traditional steps to state sync your node. Here's a script for convenience:
```bash
# Step 0: set parameters
export TRUST_HEIGHT_DELTA=10000
export MONIKER="<moniker>"
export CHAIN_ID="<chain_id>"
export PRIMARY_ENDPOINT="<rpc_endpoint>"
export SEID_HOME="/root/.sei"

# Step 1: stop seid
echo "Stopping seid process..."
systemctl stop seid

# Step 2: remove and clean up data
echo "Removing data files..."
cp $SEID_HOME/data/priv_validator_state.json /root/priv_validator_state.json
cp $SEID_HOME/config/priv_validator_key.json /root/priv_validator_key.json
cp $SEID_HOME/genesis.json /root/genesis.json
rm -rf $SEID_HOME/data/*
rm -rf $SEID_HOME/wasm
rm -rf $SEID_HOME/config/priv_validator_key.json
rm -rf $SEID_HOME/config/genesis.json
rm -rf $SEID_HOME/config/config.toml

# Step 3: seid init will create reset config and genesis
echo "Seid Init and set config..."
seid init --chain-id "$CHAIN_ID" "$MONIKER"

# Step 4: Get trusted height and hash
LATEST_HEIGHT=$(curl -s "$PRIMARY_ENDPOINT"/status | jq -r ".sync_info.latest_block_height")
if [[ "$LATEST_HEIGHT" -gt "$TRUST_HEIGHT_DELTA" ]]; then
  SYNC_BLOCK_HEIGHT=$(($LATEST_HEIGHT - $TRUST_HEIGHT_DELTA))
else
  SYNC_BLOCK_HEIGHT=$LATEST_HEIGHT
fi
SYNC_BLOCK_HASH=$(curl -s "$PRIMARY_ENDPOINT/block?height=$SYNC_BLOCK_HEIGHT" | jq -r ".block_id.hash")

# Step 5: Get persistent peers
SELF=$(cat $SEID_HOME/config/node_key.json |jq -r .id)
curl "$PRIMARY_ENDPOINT"/net_info |jq -r '.peers[] | .url' |sed -e 's#mconn://##' |grep -v "$SELF" > PEERS
PERSISTENT_PEERS=$(paste -s -d ',' PEERS)

# Step 6: Update configs for state sync
sed -i.bak -e "s|^rpc-servers *=.*|rpc-servers = \"$PRIMARY_ENDPOINT,$PRIMARY_ENDPOINT\"|" $SEID_HOME/config/config.toml
sed -i.bak -e "s|^trust-height *=.*|trust-height = $SYNC_BLOCK_HEIGHT|" $SEID_HOME/config/config.toml
sed -i.bak -e "s|^trust-hash *=.*|trust-hash = \"$SYNC_BLOCK_HASH\"|" $SEID_HOME/config/config.toml
sed -i.bak -e "s|^persistent-peers *=.*|persistent-peers = \"$PERSISTENT_PEERS\"|" $SEID_HOME/config/config.toml
sed -i.bak -e "s|^enable *=.*|enable = true|" $SEID_HOME/config/config.toml

# Step 7: Copy backed up files
cp /root/priv_validator_state.json $SEID_HOME/data/priv_validator_state.json
cp /root/priv_validator_key.json $SEID_HOME/config/priv_validator_key.json
cp /root/genesis.json $SEID_HOME/config/genesis.json

# Step 8: Restart seid
echo "Restarting seid process..."
systemctl restart seid
```

## Verification
To confirm that you are migrated to SeiDB, check your starting logs and you should see something like
`"SeiDB SC is enabled, running node with StoreV2 commit store"` in the log file.

## Rollback Steps
To rollback to use the original IAVL storage, you basically need to do 2 things:
- Disable SeiDB by setting sc-enable = false in app.toml
- Do another State Sync after the config update

## FAQ

### Where can I find the data files after migrating to SeiDB?
Before migration, all application data can be found in application.db
After migrating to SeiDB, SC data can be found in committer.db, and SS data can be found in pebbledb folder.

### After switching to SeiDB, why restarting a node takes longer time?
This is expected behavior because of the SeiDB design.
During start up, SeiDB needs to replay the changelog file from the last sc snapshot till the crash point.
This replay could usually take a few seconds to minutes based on how often sc snapshot is taken.

### Does SeiDB support archive node?
SeiDB support archive node, however there's currently no easy migration process for archive node,
so you can not convert any existing archive node to SeiDB yet.

However, archive node does get much better performance and storage efficiency if running on SeiDB.
If you want run archive node on top of SeiDB, for now, it is recommended to start running a new node with SeiDB.

### Does SeiDB support historical proof?
No, SeiDB does not support historical proof any more due to the fact that we only store raw KVs in the database.
This is one of the major trade-offs you need to make when switching to SeiDB.
