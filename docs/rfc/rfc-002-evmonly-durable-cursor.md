# RFC 002: Durable Execution Cursor for the EVM-only Application

## Changelog

- 2026-09-15: Initial draft

## Abstract

The EVM-only Autobahn application (`sei-tendermint/internal/evmonlyapp`) kept
its execution cursor, the last committed height, app hash and parent block
hash, only in memory, while the state it executed against was made durable by
FlatKV on every block. After a crash the two disagreed: the application
reported height 0, the Giga router ran `InitChain` and block 1 again, and the
node crash-looped on the nonces already in storage. This RFC records the
decision to persist the cursor alongside each block's state, in the same FlatKV
version, and to derive `Info()` from storage on construction.

## Background

In Giga mode the router (`internal/p2p/giga_router_common.go`) does not run the
CometBFT handshake. On start it calls `app.Info()`; a `LastBlockHeight` of zero
takes the fresh-genesis branch (`InitChain`, then block 1), anything else takes
the restart branch (`InitLastHeader` and pushing the last app hash). The
application therefore is the only source of truth for where execution stands.

The EVM-only executor (`giga/evmonly`) is store-backed: `ExecuteBlock` writes
receipts and calls `StateDB.CommitStateChanges(height, changesets)` before it
returns, so FlatKV state is durable once `FinalizeBlock` completes. ABCI
`Commit` then only advanced the in-memory cursor. Nothing rebuilt that cursor on
the next process start, and the `initialHeight == 1` early return in the
InitChain guard meant a non-empty store at the default initial height was never
refused.

## Discussion

### What is stored

Height needs no new storage: it is `SC().GetLatestVersion()` of the reopened
FlatKV store, which `OpenDBWithRecovery` has already converged with the block
store, state WAL and receipt store.

The app hash, parent block hash and block gas limit are stored as one record,
key `cursor` under a named changeset `evmonly`, which FlatKV routes to its misc
store under a module prefix, outside `keys.EVMStoreKey`. The record is
`height ‖ appHash ‖ parentHash ‖ gasLimit` (8 + 32 + 32 + 8 bytes). The height is
redundant with the storage version and is checked against it on load, so a
cursor that somehow belongs to a different version is refused rather than
trusted.

### How it is written

The executor gains an optional `BlockChangeSetEncoder`, called after execution
with the block context and result. Its changesets are appended to the state
encoder's output and passed to the same `CommitStateChanges` call, so the cursor
and the state it describes share a FlatKV version, a WAL entry and a recovery
outcome. The application supplies an encoder that chains the block into the app
hash (the existing SHA-256 chain over the memory-store encoding of the result,
unchanged), stages the resulting cursor as pending and returns it as the
`evmonly` changeset. `FinalizeBlock` reports the pending app hash; `Commit`
promotes pending to committed in memory, as before.

The EVM state store filters to `keys.EVMStoreKey` changesets, so the cursor
never reaches the state-sync store or the LtHash.

### How it is restored

`NewEVMOnlyApplication` reads the cursor of the store's latest version. If one
is present it builds the executor and seeds the committed cursor, so `Info()`
reports the durable height and app hash and the router takes its restart
branch. The gas limit lives in the record because `InitChain`, which used to
supply it from consensus params, is not called on restart.

`InitChain` is refused once an executor exists, and `seedInitialStateVersion`
no longer skips the check at initial height 1: a store at any version other
than zero or `initialHeight - 1` is refused. The `initialHeight - 1` case is a
store seeded by an earlier `InitChain` with no block yet, which a crash between
`InitChain` and the first block leaves behind; re-running `InitChain` there is
the fresh start the router expects.

### Crash window

A node that crashes after `FinalizeBlock(N)` returned and before `Commit(N)`
has N durable in FlatKV and restarts reporting height N, not N-1. This is the
correct answer for a store-backed executor: replaying N would fail on its own
nonces. The router's restart branch handles a durable app tip ahead of the
block store's by syncing the missing suffix.

### Alternatives considered

- Deriving the app hash from FlatKV's LtHash instead of persisting it. This
  changes what the app hash means and therefore consensus behaviour; it is left
  as a separate decision.
- Passing `RequestInitChain` to the constructor so the gas limit is available on
  restart without persisting it. Storing it with the cursor keeps the record
  self-describing and keeps the constructor's signature, which the node and
  tests already share.
- Writing the cursor in ABCI `Commit`. That reintroduces the window in which
  state is durable and the cursor is not; the cursor has to travel with the
  state.

### References

- https://github.com/sei-protocol/sei-chain/issues/4169
- `giga/evmonly/README.md`, store-backed execution
