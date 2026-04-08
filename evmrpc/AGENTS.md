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

## `sei_` and `sei2_` prefixed endpoints
Several `eth_` prefixed endpoints have a `sei_` prefixed counterpart. `eth_` endpoints only have visibility into EVM transactions, whereas `sei_` endpoints have visibility into EVM transactions plus Cosmos transactions that have synthetic EVM receipts.

The **`sei2`** namespace exposes the same **block** JSON-RPC shape as `sei` blocks, with **bank transfers** included in block payloads (HTTP only). There are seven `sei2_*` methods (block + block receipts + tx counts + `*ExcludeTraceFail` variants); there is no `sei2` transaction or filter API.

Legacy **`sei_*` and `sei2_*`** JSON-RPC (EVM HTTP only) are **gated** by the same `[evm].enabled_legacy_sei_apis` list in `app.toml` (after `deny_list`). Enforcement is **centralized** in `wrapSeiLegacyHTTP` (see `sei_legacy_http.go`): it inspects the JSON-RPC `method` field only. Wired from `HTTPServer.EnableRPC` via `HTTPConfig.SeiLegacyAllowlist` — handlers do not duplicate gate logic. Both surfaces are **deprecated** and scheduled for removal; **only methods named in that array** are allowed. `seid init` / `DefaultConfig` pre-fill the three `sei_*` address/Cosmos helpers; other gated methods (including `sei2_*`) appear **commented** in the generated template. **Docker localnet** (`docker/localnode/config/app.toml`) enables **all** gated methods except **`sei_sign`**. **HTTP 200** for all responses. **Disabled** methods return JSON-RPC `error` code `-32601`, `message` explains not enabled + deprecated, `data` `"legacy_sei_deprecated"`. **Allowed** single-object bodies pass through **unchanged**; JSON **batches** may be subset-forwarded with responses merged by `id`. Optional deprecation signal: HTTP header `Sei-Legacy-RPC-Deprecation` (`SeiLegacyDeprecationHTTPHeader` in `sei_legacy.go`). Coverage: `evmrpc/sei_legacy_test.go` and `integration_test/evm_module/rpc_io_test/testdata/sei_legacy_deprecation/*.iox`.

## `debug_` prefixed endpoints
`debug_trace*` endpoints should faithfully replay historical execution. If a transaction encountered an error during its actual execution, a `debug_trace*` call for it should reflect so. If a transction consumed X amount of gas during its actual execution, a `debug_trace*` call should show that exact amount as well.

## Consistency
RPC responses for historical heights should never change as the blockchain progresses, or as the blockchain code gets upgraded.
