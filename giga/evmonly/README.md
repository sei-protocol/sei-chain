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
receipts. The staking custom precompile is the first SDK-free implementation;
other custom precompiles are still placeholders. The open work is to port them
behind an EVM-native context that is visible to the executor's conflict tracking
without reintroducing Cosmos keeper dependencies.

## Current implementation

The `evmonly` package currently provides:

- sequential execution of the ordered block transaction list
- RLP decoding and sender recovery through go-ethereum signers
- go-ethereum `core.ApplyMessage` execution against an SDK-free `vm.StateDB`
- key-addressable state reads for balance, nonce, code, and storage
- deterministic post-block `StateChangeSet` construction
- optional executor-internal OCC for non-conflicting transaction sets
- Ethereum receipt construction with logs, bloom, gas, tx hash, block metadata,
  contract address, and effective gas price
- a map-backed `MemoryState` for tests and early integration
- fail-closed custom precompile placeholders plus an SDK-free staking precompile

The executor accepts config for nonce checks, gas-price checks, minimum gas
price, chain config, and the custom precompile registry.

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

`PrepareBlock` decodes transaction RLP and recovers senders. This work does not
touch state, so block N+1 can be prepared while block N is still executing.
`ExecuteBlock` remains the convenience path and performs prepare then execute in
one call.

The executor should be commit-neutral. It executes an ordered EVM block and
returns the state writes and receipts produced by that block. The caller owns
durable persistence, state commitment, block indexing, and receipt publication.
The concrete `Executor` accepts a `StateReader` backend through `WithState(...)`;
callers can persist the returned `ChangeSet` with a matching `StateWriter`.

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
- `Storage`: final storage slot updates or deletions

The changeset should be deterministic and serializable by the runtime layer.
The executor should not require `sdk.Context` or Cosmos stores to build it.

`Receipts` are emitted in transaction order and should contain the Ethereum
receipt fields needed by RPC and indexing: status, cumulative gas used, bloom,
logs, tx hash, contract address, and effective gas price metadata where needed.

`Txs` carries per-transaction execution metadata used to build or enrich
receipts and RPC responses. `GasUsed` is the total EVM gas consumed by the block.

## Open precompile work

Most native custom precompiles still need a separate design. If they introduce state
outside balance, nonce, code, and storage, that state must either become part of
the EVM-native changeset or be represented through an explicit extension that is
visible to the OCC conflict tracker.

The intended direction is to treat each custom precompile's migrated module
state as contract storage owned by that precompile address. With no range reads
and no side state, precompile reads and writes can then flow through ordinary
`(address, slot)` storage tracking.

The staking precompile under `giga/evmonly/precompiles/staking` follows this
shape with a byte-key store backed by storage slots owned by the staking
precompile address. Registry entries without an implementation still fail
closed with `ErrCustomPrecompilesOpen`.

## Current limitations

- The current port is sequential. The EVM-native state boundary and changeset
  shape are intended to be replaceable with the sei-v3 OCC scheduler/store.
- State input is key-addressable only. The executor lazily reads storage slots
  by `(address, slot)` and does not require or expose range iteration.
- The map-backed `MemoryState` is for tests and early integration; production
  should provide a durable native state backend.
- Historical `BLOCKHASH` lookups beyond the parent block are not wired yet.
