# Giga SS Store Migration Guide

## Overview
Giga SS Store is the next step in Sei's storage evolution on top of SeiDB. It splits the
hot EVM state into its own dedicated state-store (SS) database so the node can scale to
**~150k TPS** target throughput. Migrating repartitions the SS layer into two cooperating
stores:

| Layer | Cosmos backend | EVM backend |
|-------|----------------|-------------|
| **SC** (State Commit, app hash) | memiavl | FlatKV |
| **SS** (State Store, historical queries) | single MVCC DB (Pebble/Rocks) | dedicated EVM SS MVCC DB(s) under `data/evm_ss/` |

Only the **SS** layer changes for this migration. SC layer config is unaffected.

## Prerequisite
- This migration guide is for **RPC nodes only**. Validator nodes and archive nodes are
  not supported by this migration flow yet.
- Migrating to Giga SS Store **requires a full state sync**. There is no in-place
  migration path and no live "dual-write then split" workflow. A state sync wipes the
  local data directory and imports a fresh snapshot into the new layout.
- `sc-enable = true` and `ss-enable = true`. Both must be enabled for this migration.

## Benefits
- EVM reads served exclusively from a dedicated EVM SS database.
- Non-EVM modules no longer pay write amplification for EVM state.
- Backend change (PebbleDB ↔ RocksDB) can be combined with the same state sync since
  `ss-backend` drives both the Cosmos SS MVCC DB and the EVM SS sub-DBs.

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

# DBBackend for the Cosmos SS MVCC DB and for every EVM SS sub-DB.
# Supported: pebbledb, rocksdb. Default pebbledb.
ss-backend = "pebbledb"

# Route EVM state to the dedicated EVM SS backend.
# When false (default), EVM state lives in the Cosmos SS backend alongside everything
# else. When true, EVM data is routed exclusively to the EVM SS backend; non-EVM data
# stays in Cosmos SS. No fallback between backends.
evm-ss-split = true
```

If you are switching backend in the same step:
- PebbleDB → RocksDB: set `ss-backend = "rocksdb"`, build with `-tags rocksdbBackend`,
  and install RocksDB per the [SeiDB Migration Guide](./seidb_migration.md#step-2-tune-configs-based-on-node-role).
- No data migration tool is needed across backends — the state sync populates the new
  layout.

### Step 2: State Sync
Giga SS Store is fully compatible with the existing state snapshot format. On import,
the composite state store routes each snapshot node based on the importing node's
`evm-ss-split`:

- With `evm-ss-split = true`, EVM snapshot nodes go only into EVM SS and non-EVM nodes
  go only into Cosmos SS.
- The import path normalizes legacy `evm_flatkv` snapshot nodes to `evm`, so snapshots
  produced by either the old or new FlatKV module are accepted.

Both stores end up fully populated at the snapshot height, so the node can start
serving reads immediately.

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
To confirm Giga SS Store is active, check the startup logs for both:

- `"SeiDB SS is enabled"` with the configured `backend`.
- `"SeiDB EVM StateStore optimization is enabled"` with the `separateDBs` label.
- `"EVM state store enabled"` from the composite store constructor with the `dir`
  and `separateDBs` labels.

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
   already has committed history but the EVM SS directory (`data/evm_ss/` by default)
   does not exist or is empty. Running before the sub-DBs are opened means a rejected
   config does not leave a confusing empty `data/evm_ss/` behind.

2. **EVM SS DB empty post-open, pre-recovery.** Belt-and-suspenders for (1) when the
   directory exists but its DBs are empty. The WAL only covers the last `KeepRecent`
   blocks so replay cannot rebuild a fresh EVM SS from scratch.

3. **Mismatched earliest versions, post-recovery.** If the two DBs were populated from
   different snapshots (or pruned independently), historical reads would be
   inconsistent. A non-zero earliest-version divergence aborts startup.

If any check fires, the correct fix is either (a) complete the state sync described
above, or (b) set `evm-ss-split = false`. If `data/evm_ss/` is stale from a failed
attempt, remove it before state syncing.

## Rollback Steps
To roll back:
- Set `evm-ss-split = false` in `app.toml`.
- Restart the node. The EVM SS DB under `data/evm_ss/` will not be opened but will
  remain on disk until manually removed.

To fully reclaim EVM SS disk usage, stop the node and delete `data/evm_ss/` after
reverting the setting.

## FAQ

### Where can I find the data files after migrating?
- Cosmos SS data lives under the same directory as before (typically `data/pebbledb/`
  for the default `pebbledb` backend).
- EVM SS data lives under `data/evm_ss/`.
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

### Does Giga SS Store support historical proofs?
No, same as SeiDB. SS stores raw KVs and does not reconstruct IAVL-style proofs.
