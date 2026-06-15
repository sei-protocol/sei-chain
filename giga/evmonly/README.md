# EVM-only execution scaffold

This package sketches the EVM-only execution boundary for the final-form giga
path. It is intentionally separate from the current Cosmos-backed giga wiring in
`app/app.go`.

The target execution model is based on the `sei-v3` executor:

- raw transaction bytes are Ethereum RLP transactions, not Cosmos SDK txs
- state layout is EVM-native: balance, storage, code, and nonce are keyed by
  EVM addresses and hashes
- block execution can overlap with parsing or persistence work for nearby blocks
- hot execution should not allocate `sdk.Context`, `sdk.Tx`,
  `MsgEVMTransaction`, Cosmos messages, or Cosmos coins

Custom precompiles are intentionally not implemented in this scaffold. The open
work is to port them behind an EVM-native context that is visible to the
executor's conflict tracking without reintroducing Cosmos keeper dependencies.

## Executor interface

The boundary is:

```go
ExecuteBlock(context.Context, BlockRequest) (*BlockResult, error)
```

The executor should be commit-neutral. It executes an ordered EVM block and
returns the state writes and receipts produced by that block. The caller owns
durable persistence, state commitment, block indexing, and receipt publication.

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

Native custom precompiles still need a separate design. If they introduce state
outside balance, nonce, code, and storage, that state must either become part of
the EVM-native changeset or be represented through an explicit extension that is
visible to the OCC conflict tracker.
