# Giga SS Store Migration Guide

## Overview
Giga SS Store is the next step in Sei's storage evolution on top of SeiDB. It splits the
hot EVM state into its own dedicated state-store (SS) database so the node can scale to
**~150k TPS** target throughput. Migrating repartitions the SS layer into two cooperating
stores:

| Layer | Cosmos backend | EVM backend |
|-------|----------------|-------------|
| **SC** (State Commit, app hash) | memiavl | FlatKV |
| **SS** (State Store, historical queries) | single MVCC DB (Pebble/Rocks) under `data/state_store/cosmos/{backend}` | dedicated EVM SS MVCC DB under `data/state_store/evm/{backend}` |

Only the **SS** layer changes for this migration. SC layer config is unaffected.

## Prerequisite
- This migration guide is for **RPC nodes only**. Validator nodes and archive nodes are
  not supported by this migration flow yet.
- Migrating to Giga SS Store **requires a full state sync**, or restoring a
  data-directory snapshot taken from a node that already has Giga SS Store enabled.
  There is no in-place migration path and no live "dual-write then split" workflow.
  A state sync wipes the local data directory and imports a fresh snapshot into the
  new layout.
- `sc-enable = true` and `ss-enable = true`. Both must be enabled for this migration.

## Benefits
- EVM reads served exclusively from a dedicated EVM SS database.
- Non-EVM modules no longer pay write amplification for EVM state.
- Backend change (PebbleDB ↔ RocksDB) can be combined with the same state sync since
  `ss-backend` drives both the Cosmos SS MVCC DB and the EVM SS DB.

## What's different about EVM SS
EVM SS is **point-query only by design** (`Get` / `Has`). Iteration is explicitly
disabled on the EVM backend for performance — the hot EVM read path is tuned for
direct key lookups, and cross-bucket scans would defeat the per-type sub-DB layout.
Any EVM read that needs iteration must be kept on the Cosmos SS side.

## Migration Steps

### Step 1: Add Configurations
Apply the following settings in `app.toml` (usually `~/.sei/config/app.toml`):

```toml
[state-commit]
# State commit is untouched by this migration.
sc-enable = true

[state-store]
ss-enable = true

# DBBackend for the Cosmos SS MVCC DB and for the EVM SS DB.
# Supported: pebbledb, rocksdb. Default pebbledb.
ss-backend = "pebbledb"

# Route EVM state to the dedicated EVM SS backend.
# When false (default), EVM state lives in the Cosmos SS backend alongside everything
# else. When true, EVM data is routed exclusively to the EVM SS backend; non-EVM data
# stays in Cosmos SS. No fallback between backends.
evm-ss-split = true

# Split EVM key families across multiple DBs inside the EVM SS directory.
# Default false: all EVM state lives in a single DB (the recommended, safer layout).
# Setting true can improve performance but is experimental and not fully tested.
# Leave this at false unless you are deliberately evaluating that path.
evm-ss-separate-dbs = false
```

If you are switching backend in the same step:
- PebbleDB → RocksDB: set `ss-backend = "rocksdb"`, build with `-tags rocksdbBackend`,
  and install RocksDB per the [SeiDB Migration Guide](./seidb_migration.md#step-2-tune-configs-based-on-node-role).
- No data migration tool is needed across backends — the state sync populates the new
  layout.

### Step 2: State Sync
Giga SS Store is fully compatible with the existing **P2P state-sync** snapshot format.
On import, the composite state store routes each snapshot node based on the importing
node's `evm-ss-split`:

- With `evm-ss-split = true`, EVM snapshot nodes go only into EVM SS and non-EVM nodes
  go only into Cosmos SS.
- The import path normalizes legacy `evm_flatkv` snapshot nodes to `evm`, so snapshots
  produced by either the old or new FlatKV module are accepted.

Both stores end up fully populated at the snapshot height, so the node can start
serving reads immediately.

P2P state-sync snapshots (the chunks peers serve over the network) are **not**
layout-sensitive. Giga SS Store only changes how the importing node writes those
nodes onto disk.

**Data-directory snapshots** (a tar of `~/.sei/data`) *are* layout-sensitive. A
tarball taken from a Giga SS node contains `data/state_store/evm/` (and
`data/state_store/cosmos/`) instead of a single mixed Cosmos SS directory. You can
enable Giga SS Store by restoring such a snapshot and setting `evm-ss-split = true`;
you do not need to P2P state sync again. A tarball from a non-Giga node cannot be
used this way — convert those nodes with the state sync flow below.

Snapshot hosts that publish `data/` tarballs may offer both Giga SS and non-Giga
copies while the fleet migrates. Use a Giga SS snapshot only when you intend to run
with `evm-ss-split = true`.

Use the state sync flow documented in the
[SeiDB Migration Guide](./seidb_migration.md#step-3-state-sync). Minimal shape:

```bash
export TRUST_HEIGHT_DELTA=10000
export MONIKER="<moniker>"
export CHAIN_ID="<chain_id>"
export PRIMARY_ENDPOINT="<rpc_endpoint>"
export SEID_HOME="/root/.sei"

# 1. Stop seid
systemctl stop seid

# 2. Back up files you need to preserve and wipe local state
cp $SEID_HOME/data/priv_validator_state.json /root/priv_validator_state.json
cp $SEID_HOME/config/priv_validator_key.json /root/priv_validator_key.json
cp $SEID_HOME/genesis.json /root/genesis.json
rm -rf $SEID_HOME/data/*
rm -rf $SEID_HOME/wasm
rm -rf $SEID_HOME/config/priv_validator_key.json
rm -rf $SEID_HOME/config/genesis.json
rm -rf $SEID_HOME/config/config.toml

# 3. Re-init, update config.toml and app.toml (set Giga SS Store values from Step 1)
seid init --chain-id "$CHAIN_ID" "$MONIKER"

# 4. Resolve trust height/hash and persistent peers against PRIMARY_ENDPOINT,
#    then update config.toml (see SeiDB Migration Guide for the full snippet).

# 5. Restore the backed up files
cp /root/priv_validator_state.json $SEID_HOME/data/priv_validator_state.json
cp /root/priv_validator_key.json $SEID_HOME/config/priv_validator_key.json
cp /root/genesis.json $SEID_HOME/config/genesis.json

# 6. Start seid
systemctl restart seid
```

## Verification
To confirm Giga SS Store is active, check the startup logs. With the default
(recommended) `evm-ss-separate-dbs = false`, they look like:

```
msg="SeiDB SS is enabled" backend=pebbledb
msg="SeiDB EVM StateStore optimization is enabled" separateDBs=false
msg="EVM state store enabled" logger=db/state-db/ss/composite dir=/home/<user>/.sei/data/state_store/evm/pebbledb separateDBs=false
```

`separateDBs=false` is expected. It means EVM state is stored in a **single** DB
inside `data/state_store/evm/{backend}`, not split across per-type sub-DBs. That
is the default, for safety. `separateDBs=true` appears only if you opted into the
experimental `evm-ss-separate-dbs = true` setting.

On an RPC node, confirm `debug_traceBlockByNumber` succeeds after state sync completes:

```bash
curl -s -X POST http://127.0.0.1:8545 \
  -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","method":"debug_traceBlockByNumber","params":["latest",{}],"id":1}'
```

The response should contain a `"result"` field rather than an RPC error.

## Safety Checks
Three DB-state checks run at startup and refuse to launch the node if the EVM SS and
Cosmos SS DBs are inconsistent. They specifically catch the footgun of flipping
`evm-ss-split` from `false` to `true` without state syncing.

1. **EVM SS directory missing or empty** (before the EVM SS is opened). When
   `evm-ss-split = true`, `NewCompositeStateStore` refuses to proceed if Cosmos SS
   already has committed history but the EVM SS directory
   (`data/state_store/evm/{backend}` by default) does not exist or is empty. Running
   before the DB is opened means a rejected config does not leave a confusing empty
   `data/state_store/evm/` behind.

2. **EVM SS DB empty post-open, pre-recovery.** Belt-and-suspenders for (1) when the
   directory exists but its DBs are empty. The WAL only covers the last `KeepRecent`
   blocks so replay cannot rebuild a fresh EVM SS from scratch.

3. **Mismatched earliest versions, post-recovery.** If the two DBs were populated from
   different snapshots (or pruned independently), historical reads would be
   inconsistent. A non-zero earliest-version divergence aborts startup.

If any check fires, the correct fix is either (a) complete the state sync described
above, or (b) set `evm-ss-split = false`. If `data/state_store/evm/` is stale from a
failed attempt, remove it before state syncing.

## Rollback Steps
To roll back:
- Set `evm-ss-split = false` in `app.toml`.
- Restart the node. The EVM SS DB under `data/state_store/evm/` will not be opened
  but will remain on disk until manually removed.

To fully reclaim EVM SS disk usage, stop the node and delete `data/state_store/evm/`
after reverting the setting. Nodes that still have the legacy `data/evm_ss/`
directory should delete that instead — the node keeps using `data/evm_ss/` if it
already exists.

## FAQ

### Where can I find the data files after migrating?
- Cosmos SS data lives under `data/state_store/cosmos/{backend}` after this
  migration (e.g. `data/state_store/cosmos/pebbledb`). Nodes that already had
  `data/pebbledb/` keep using that legacy path; a wipe + state sync uses the new
  layout.
- EVM SS data lives under `data/state_store/evm/{backend}` (e.g.
  `data/state_store/evm/pebbledb`). The legacy path `data/evm_ss/` is used only if
  that directory already exists on disk.
- SC data (memiavl + FlatKV) is untouched by this migration.

### Does Giga SS Store change the app hash or consensus?
No. The SC layer is unchanged, so memiavl remains the authoritative source for the app
hash. Giga SS Store is a per-node SS change that is invisible to the network.

### Can I migrate a validator node with this guide?
Not yet. This migration guide is for RPC nodes only.

### Can I migrate an archive node with this guide?
Not yet. Archive-node migration is out of scope for this guide.

### Can I toggle back to `evm-ss-split = false` after enabling it?
Yes, but cleanly rolling back requires another state sync — under `evm-ss-split = true`,
EVM writes go only to the EVM SS DB, so Cosmos SS will not have those writes. Setting
`evm-ss-split = false` and restarting works to stop opening the EVM SS DB, but queries
for EVM state will miss anything written after the Giga state sync until you state
sync again.

### Why can't I just flip `evm-ss-split = true` on a running node?
`evm-ss-split = true` requires the EVM SS DB to already contain the full history that
Cosmos SS has. A live flip would leave the EVM SS DB empty while the composite store
refuses to fall back to Cosmos SS, which would translate into missing EVM state at
query time. The safety checks above block this scenario at startup.

### Does `separateDBs=false` in the startup log mean Giga SS Store is off?
No. `separateDBs` is the `evm-ss-separate-dbs` flag, not `evm-ss-split`. With Giga
SS Store enabled (`evm-ss-split = true`) and the default `evm-ss-separate-dbs =
false`, EVM state lives in a dedicated EVM SS directory as a single DB. Splitting
EVM into per-type sub-DBs (`evm-ss-separate-dbs = true`) is experimental.

### Can I enable Giga SS Store from a data-directory snapshot instead of P2P state sync?
Yes, if the snapshot is a `data/` tarball taken from a node that already has Giga SS
Store enabled. Restore it, set `evm-ss-split = true`, and start. P2P state-sync
snapshots are unaffected either way — they do not embed the on-disk SS layout.

### Does Giga SS Store support historical proofs?
No, same as SeiDB. SS stores raw KVs and does not reconstruct IAVL-style proofs.
