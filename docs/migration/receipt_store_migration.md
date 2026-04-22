# Parquet Receipt Store Migration Guide

## Overview
The receipt store holds EVM transaction receipts and logs used to serve
`eth_getTransactionReceipt`, `eth_getTransactionByHash`, `eth_getLogs`, and
related RPC methods. Historically this data lived in a PebbleDB-backed MVCC
store. The parquet receipt store swaps the per-block MVCC layout for columnar
parquet files with a PebbleDB-backed `tx_hash -> block_number` index, dropping
disk footprint and speeding up log range queries without changing any RPC
semantics.

| Layer | Legacy backend | New backend |
|-------|----------------|-------------|
| Receipt & log storage | single PebbleDB MVCC DB | append-only parquet files under `data/ledger/receipt/parquet/` |
| `tx_hash` lookup | same PebbleDB | dedicated PebbleDB index under `data/ledger/receipt/parquet/tx-hash-index/` |

Only the receipt store changes. State commit (SC), state store (SS), and
anything else on disk are untouched.

## Prerequisite
- This migration guide is for **RPC nodes only**. Validator nodes and archive
  nodes are not supported by this migration flow. Validators do not serve EVM
  receipt RPCs, and archive nodes require full receipt history which this
  migration path does not preserve.
- The node must be at a version that includes the parquet receipt backend
  (`rs-backend = "parquet"`).

## Benefits
- Columnar parquet layout cuts receipt-store disk usage substantially vs. the
  legacy PebbleDB MVCC layout.
- `eth_getLogs` range scans read parquet column chunks instead of iterating
  MVCC keys, improving latency for wide block/address/topic filters.
- The PebbleDB `tx_hash -> block_number` index narrows `eth_getTransactionReceipt`
  and `eth_getTransactionByHash` lookups to a single parquet file instead of a
  full scan across all files.

## Tradeoff: historical receipts are dropped
Deleting the legacy `receipt.db` wipes every receipt and log the node has on
disk. After restart, the parquet receipt store is populated **from the restart
height forward only**. RPC calls for transactions or logs from before the
restart height will return "not found" until the node re-ingests that range
(e.g., via a new state sync snapshot that covers it) or the legacy DB is
restored.

If you need full historical receipts on this node, do not follow this
migration — wait for archive-node support.

## Migration Steps

### Step 1: Stop seid
```bash
systemctl stop seid
```

### Step 2: Delete the existing receipt store directory
The legacy path is `$SEID_HOME/data/receipt.db`. On newer nodes already using
the reorganized data layout (see
[Restructure sei data folder for Giga](https://github.com/sei-protocol/sei-chain/pull/3155)),
the receipt store instead lives under `$SEID_HOME/data/ledger/receipt/pebbledb/`.
Remove whichever one exists:

```bash
# Legacy layout
rm -rf $SEID_HOME/data/receipt.db

# New layout (post data-folder restructure)
rm -rf $SEID_HOME/data/ledger/receipt/pebbledb
```

Removing the directory is what makes the node resolve to the new parquet
path on the next start; leaving the pebble directory in place would cause the
parquet backend to write alongside it and waste disk.

### Step 3: Configure parquet in `app.toml`
Apply the following settings in `app.toml` (usually
`~/.sei/config/app.toml`):

```toml
[receipt-store]
# Switch the receipt store backend from pebbledb to parquet.
# Supported: pebbledb, parquet. Default pebbledb.
rs-backend = "parquet"

# Leave empty to use the default location: data/ledger/receipt/parquet/
db-directory = ""

# Ignored by the parquet backend (parquet writes are synchronous).
async-write-buffer = 100

# How often the background pruner runs. Receipt retention is driven by the
# global min-retain-blocks flag; this only controls the prune cadence.
prune-interval-seconds = 600

# tx_hash -> block_number index backing store. "pebbledb" keeps parquet
# lookups O(1) per hash; "" falls back to a full DuckDB scan of parquet
# files for every receipt-by-hash call and is not recommended.
tx-index-backend = "pebbledb"
```

### Step 4: Restart seid
```bash
systemctl restart seid
```

The RPC node will begin writing parquet files and the PebbleDB tx-hash index
at `data/ledger/receipt/parquet/` from the first block committed after start.
Historical receipts prior to that height are not backfilled.

## Verification
After restart, confirm parquet is active:

1. Check the startup logs for a line from the `db/ledger-db/receipt` logger
   indicating the parquet store opened (no `receipt store db directory not
   configured` or `unsupported receipt store backend` error).
2. Confirm the new files exist on disk once the node has committed a block:
   ```bash
   ls $SEID_HOME/data/ledger/receipt/parquet/
   # Expect: receipts_*.parquet, logs_*.parquet, tx-hash-index/
   ```
3. Issue a receipt-by-hash RPC for a transaction **committed after the
   restart height** and confirm it returns the receipt:
   ```bash
   curl -s -X POST http://127.0.0.1:8545 \
     -H 'Content-Type: application/json' \
     --data '{"jsonrpc":"2.0","method":"eth_getTransactionReceipt","params":["<post-restart-tx-hash>"],"id":1}'
   ```

Requests for transactions from before the restart height will return `null`
until the node catches up, which is expected given the data was deleted in
Step 2.

## Rollback Steps
To roll back to the PebbleDB receipt store:

1. Stop seid.
2. Set `rs-backend = "pebbledb"` in `app.toml`.
3. Delete the parquet data:
   ```bash
   rm -rf $SEID_HOME/data/ledger/receipt/parquet
   ```
4. Restart seid.

Rollback has the same tradeoff as the forward migration — receipts written to
parquet between the forward migration and the rollback are lost, and the
pebble receipt store will start filling from the rollback height forward.

## FAQ

### Can I migrate a validator node with this guide?
No. Validators do not serve receipt-based EVM RPCs and are not a supported
target for this migration.

### Can I migrate an archive node with this guide?
No. Archive nodes require full receipt history and this migration deletes it.

### Do I need to re-run state sync?
No. The receipt store is independent of SC (memiavl) and SS (Cosmos/EVM state
store). Deleting the receipt directory only drops receipts and logs; chain
state is unaffected.

### Will my node re-download or backfill historical receipts?
No. The parquet store starts empty and fills forward from the restart height.
There is no replay path that rebuilds receipts from chain state. If you need
history, use a node that already has the receipts or wait for archive-node
support.

### Can I keep the old `receipt.db` around as a fallback?
Not usefully. The receipt store is opened by a single backend at a time. A
leftover `receipt.db` would not be consulted by the parquet backend on reads.
If disk is tight, delete it; if you want the option to roll back, keep it
but understand it will not serve reads while parquet is active.

### What does `tx-index-backend = "pebbledb"` cost me?
A small PebbleDB under `data/ledger/receipt/parquet/tx-hash-index/` that
stores `tx_hash -> block_number` for every receipt written. It is pruned on
the same cadence as parquet files (`prune-interval-seconds`) and scoped by
`min-retain-blocks`. Setting it to `""` removes the PebbleDB but forces a
DuckDB scan across all parquet files for each receipt-by-hash lookup and is
not recommended for RPC nodes.

### Does switching receipt backends change the app hash or consensus?
No. Receipts are not in the app hash. This migration is a per-node RPC-layer
change and is invisible to the network.
