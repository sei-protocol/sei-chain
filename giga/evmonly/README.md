# EVM-only giga execution

This package contains the EVM-only execution boundary for the final-form giga
path. It is intentionally separate from the current Cosmos-backed giga wiring in
`app/app.go`.

The target execution model is based on the `sei-v3` executor:

- raw transaction bytes are Ethereum RLP transactions, not Cosmos SDK txs
- state layout is EVM-native: balance, storage, code, and nonce are keyed by
  EVM addresses and hashes
- block execution can overlap with parsing or persistence work for nearby blocks
- hot execution should not allocate `sdk.Context`, `sdk.Tx`,
  `MsgEVMTransaction`, Cosmos messages, or Cosmos coins

The current implementation executes raw RLP transactions with go-ethereum
against an EVM-native state backend, then returns a changeset plus Ethereum
receipts. Custom precompiles are still placeholders. The open work is to port
them behind an EVM-native context that is visible to the executor's conflict
tracking without reintroducing Cosmos keeper dependencies.

## Current implementation

The `evmonly` package currently provides:

- sequential execution of the ordered block transaction list
- parallel RLP decoding and sender recovery through go-ethereum signers
- go-ethereum `core.ApplyMessage` execution against an SDK-free `vm.StateDB`
- key-addressable state reads for balance, nonce, code, and storage
- deterministic post-block `StateChangeSet` construction
- optional executor-internal Block-STM-style execution for optimistic parallel
  transaction execution with granular validation and reruns
- Ethereum receipt construction with logs, bloom, gas, tx hash, block metadata,
  contract address, and effective gas price
- a map-backed `MemoryState` for tests and early integration
- fail-closed custom precompile placeholders

The executor accepts config for nonce checks, gas-price checks, minimum gas
price, chain config, parse workers, OCC workers, result pooling, and the custom
precompile registry. A nil chain config defaults to geth's
`params.AllDevChainProtocolChanges`, which is convenient for tests and scaffold
wiring; production callers should provide the chain's explicit EVM config.

## Executor interface

The boundary is:

```go
ExecuteBlock(context.Context, BlockRequest) (*BlockResult, error)
```

For callers that can pipeline stateless work across blocks, the concrete
executor also supports:

```go
PrepareBlock(context.Context, BlockRequest) (PreparedBlock, error)
ExecutePreparedBlock(context.Context, PreparedBlock) (*BlockResult, error)
```

`PrepareBlock` decodes transaction RLP and recovers senders using
`ParseWorkers` goroutines while preserving transaction order and deterministic
first-error reporting. This work does not touch state, so block N+1 can be
prepared while block N is still executing. `ExecuteBlock` remains the
convenience path and performs prepare then execute in one call. `PreparedBlock`
is trusted executor-produced data: callers should pass the result of
`PrepareBlock` unchanged, because `ExecutePreparedBlock` does not recover
senders again.

The executor should be commit-neutral. It executes an ordered EVM block and
returns the state writes and receipts produced by that block. The caller owns
durable persistence, state commitment, block indexing, and receipt publication.
The concrete `Executor` accepts a `StateReader` backend through `WithState(...)`;
callers can persist the returned `ChangeSet` with a matching `StateWriter`.
When OCC is enabled, `StateReader` may be read concurrently; backends can
implement `ConcurrentStateReader` to opt out of executor-side read locking.
Call `Close()` to disable future OCC execution on an executor.

A non-nil `error` means block validation failed and the caller must not commit a
partial output. EVM call failures inside an otherwise valid transaction are
represented in `Receipts` and `Txs` with failed status.

## Input block format

`BlockRequest` is the expected input:

- `Context` contains block-constant EVM fields:
  - block number
  - timestamp
  - block gas limit
  - chain ID
  - base fee
  - blob base fee, when enabled
  - coinbase
  - parent hash
  - current block hash
  - prevRandao
- `Txs` is the canonical, already-ordered transaction list for the block.
- Each tx is raw Ethereum transaction RLP bytes.
- There is no Cosmos SDK tx envelope, `MsgEVMTransaction`, ante wrapper, memo,
  fee object, account address mapping object, or Cosmos gas meter in the input.

The runtime or consensus layer is responsible for choosing the block order and
providing the block context. The executor is responsible for parsing tx RLP,
recovering senders, validating EVM nonce/fee/intrinsic-gas rules, executing EVM
state transitions, and producing deterministic outputs.

`BlockHash` is used for receipts and log metadata. The current `BLOCKHASH`
opcode callback only exposes `ParentHash`; older historical hashes require a
runtime-provided hash source in a later integration step.

## Output format

`BlockResult` contains two primary outputs:

- `ChangeSet`: the EVM-native state writes produced by the block.
- `Receipts`: Ethereum receipts for the executed transactions.

`ChangeSet` is expressed as post-block values, not deltas:

- `Balances`: final balance for each changed EVM address
- `Nonces`: final nonce for each changed EVM address
- `Code`: final bytecode updates or deletions
- `StorageClears`: addresses whose persisted storage must be fully cleared
- `Storage`: final storage slot updates or deletions

The changeset should be deterministic and serializable by the runtime layer.
The executor should not require `sdk.Context` or Cosmos stores to build it.
State writers should apply `StorageClears` before per-slot `Storage` updates.

`Receipts` are emitted in transaction order and should contain the Ethereum
receipt fields needed by RPC and indexing: status, cumulative gas used, bloom,
logs, tx hash, contract address, and effective gas price metadata where needed.

`Txs` carries per-transaction execution metadata used to build or enrich
receipts and RPC responses. `GasUsed` is the total EVM gas consumed by the block.

## Open precompile work

Native custom precompiles still need a separate design. If they introduce state
outside balance, nonce, code, and storage, that state must either become part of
the EVM-native changeset or be represented through an explicit extension that is
visible to the OCC conflict tracker.

The intended direction is to treat each custom precompile's migrated module
state as contract storage owned by that precompile address. With no range reads
and no side state, precompile reads and writes can then flow through ordinary
`(address, slot)` storage tracking.

Until that design is implemented, the `evmonly` executor accepts a custom
precompile registry only as a fail-closed placeholder. Calls to registered
custom precompile addresses return `ErrCustomPrecompilesOpen`.

## Block-STM execution

When `OCCWorkers > 1`, there is more than one transaction, and custom precompiles
are not enabled, the executor attempts optimistic parallel execution. The
Block-STM scheduler runs two pools: an execution pool and a validation pool.
Initial incarnations for all transactions are queued into the execution pool
against the base state. Every completed incarnation is queued into the validation
pool, where validation compares its recorded balance, nonce, code, account, and
`(address, slot)` storage reads/writes against writes accepted after that
incarnation's source prefix.

- transactions with no dependency on newly accepted prior writes are retained
  until they can be accepted in block order
- transactions whose reads, writes, gas-pool availability, or execution errors
  are invalidated by the accepted prefix are queued back into the execution pool
  as a new incarnation against an immutable snapshot of that prefix
- reruns can execute while earlier transaction validations are still progressing,
  but final acceptance still happens in block order
- rerun outputs replace only that transaction's prior incarnation
- the final changeset is generated from the accepted prefix state, not by
  blindly merging mixed-base speculative writes

This is intentionally conservative. A conflict can cause extra reruns, but it
should not cause a whole-block sequential fallback. The current incarnation cap
is 10; if a transaction still cannot validate by then, the executor falls back to
the sequential path and marks `OCCStats.Fallback`. Coinbase fee credits are
tracked as commutative balance deltas, so independent fee-paying transactions do
not conflict only because they reward the same coinbase. If a later transaction
reads or writes that balance, spends funds made available by that fee credit, or
mixes a normal balance write with a fee credit to the same address, it is rerun
against the updated prefix.

Speculative execution reuses scratch `nativeStateDB` instances from an executor
pool. The returned receipts, logs, read/write sets, commutative balance deltas,
and changesets are detached before the scratch state DB is reset. EVM snapshots
inside `nativeStateDB` are journaled by mutation kind instead of cloning all side
state at every snapshot; account contents, access lists, transient storage,
tx-storage bookkeeping, preimages, finalise markers, and commutative balance
deltas are restored by undo entries.

`OCCStats` reports whether optimistic execution was attempted, how many reruns
were needed, and aggregated conflict samples. `Fallback` is reserved for cases
where the executor gives up on the optimistic path, currently because the
incarnation cap was reached; ordinary conflicts should be resolved by
per-transaction reruns instead.

## Current limitations

- Block-STM execution is optional and conservative; conflicts are resolved by
  granular reruns, but the validator may rerun more transactions than a less
  conservative implementation would.
- State input is key-addressable only. The executor lazily reads storage slots
  by `(address, slot)` and does not require or expose range iteration.
- The map-backed `MemoryState` is for tests and early integration; production
  should provide a durable native state backend.
- Historical `BLOCKHASH` lookups beyond the parent block are not wired yet.
- Block-level blob gas accounting and `MaxBlobGasPerBlock` enforcement are not
  wired yet, so blob transactions are rejected fail-closed.
