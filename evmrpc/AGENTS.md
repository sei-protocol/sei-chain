# EVM RPC SPECS
EVM RPCs live under `evmrpc/` folder.

EVM RPCs prefixed by `eth_` and `debug_` on Sei generally follows [Ethereum's spec](https://www.quicknode.com/docs/ethereum/api-overview). However, there are some notable distinctions.

- **Pending** - Sei has instant finality and thus has no concept of `pending` blocks. However, the RPCs still accept `pending` for applicable parameters, and will treat it equivalent to `final`/`safe`/`latest`.
- **No Uncle** - Sei does not have the concept of uncle blocks, so any endpoint relevant to uncle is not supported.
- **No Trie** - Sei does not store states in a trie, so any endpoint relevant to the trie data structure is not supported.
- **No PoW** - Sei has never used proof-of-work, so endpoints like `eth_mining` and `eth_hashrate` are not supported.
- **No Blobs** - Sei does not support EIP-4844 blob transactions. `eth_blobBaseFee` returns JSON-RPC error code `-32000` with message `blobs not supported on this chain`.
- **Explicitly unsupported RPCs (same `-32000` pattern)** — Methods are registered so clients get a clear error instead of `-32601` method not found:
  - `debug_getRawBlock`, `debug_getRawHeader`, `debug_getRawReceipts`, `debug_getRawTransaction`
  - `eth_newPendingTransactionFilter`
  - `eth_syncing`

## `sei_` prefixed endpoints
Several `eth_` prefixed endpoints have a `sei_` prefixed counterpart. `eth_` endpoints only have visibility into EVM transactions, whereas `sei_` endpoints have visibility into EVM transactions plus Cosmos transactions that have synthetic EVM receipts.

## `debug_` prefixed endpoints
`debug_trace*` endpoints should faithfully replay historical execution. If a transaction encountered an error during its actual execution, a `debug_trace*` call for it should reflect so. If a transction consumed X amount of gas during its actual execution, a `debug_trace*` call should show that exact amount as well.

## Consistency
RPC responses for historical heights should never change as the blockchain progresses, or as the blockchain code gets upgraded.
