# EVM RPC SPECS
EVM RPCs live under `evmrpc/` folder.

## HTTP middleware order (JSON-RPC)

When JWT is configured, unauthenticated requests are rejected before the byte
budget is touched:

```
jwt → requestSizeLimiter → rateLimitMiddleware → seiLegacyHTTPGate → gzip → vhost → cors → rpc.Server
```

Without JWT:

```
requestSizeLimiter → rateLimitMiddleware → seiLegacyHTTPGate → gzip → vhost → cors → rpc.Server
```

`requestSizeLimiter` caps each body with `http.MaxBytesReader`, charges the
global `max_concurrent_request_bytes` budget incrementally as body bytes are
read (64 KiB batches), and enforces `body_read_idle_timeout` between body
chunks via an idle timer that only sets the connection read deadline when a
stall actually expires (HTTP 408 on stall, HTTP 429 on mid-read budget
exhaustion). net/http's `ReadTimeout` is left untouched during normal reads.

EVM RPCs prefixed by `eth_` and `debug_` on Sei generally follows [Ethereum's spec](https://www.quicknode.com/docs/ethereum/api-overview). However, there are some notable distinctions.

- **Pending** - Sei has instant finality and thus has no concept of `pending` blocks. However, the RPCs still accept `pending` for applicable parameters, and will treat it equivalent to `final`/`safe`/`latest`.
- **No Uncle** - Sei does not have the concept of uncle blocks, so any endpoint relevant to uncle is not supported.
- **No Trie** - Sei does not store states in a trie, so any endpoint relevant to the trie data structure is not supported.
- **No PoW** - Sei has never used proof-of-work, so endpoints like `eth_mining` and `eth_hashrate` are not supported.
- **No Blobs** - Sei does not support EIP-4844 blob transactions. `eth_blobBaseFee` returns JSON-RPC error code `-32000` with message `blobs not supported on this chain`.
- **`milliTimestamp`** — BEP-520-compatible block-header field carrying the header time as a hex quantity of **Unix milliseconds**, alongside the standard `timestamp`, which stays in **whole seconds** because every Ethereum client reads it that way. Sei block intervals are shorter than a second, so `timestamp` repeats across consecutive blocks. `milliTimestamp` is **not** unique per block either: under Autobahn a proposal spaces consecutive blocks by 1µs (`minTimestampDiff`), so blocks less than a millisecond apart tie. Unrelated to the seconds-based values feeding fork rules and the `TIMESTAMP` opcode (`vm.BlockContext.Time`, `MakeSigner`, `ethtypes.Header.Time`) — those must stay in seconds.
- **Four separate encoders build block headers.** A field added to one is silently absent from the other three, so a new header field has to be added to all four:
  - `EncodeTmBlock` — `eth_getBlockByNumber` / `eth_getBlockByHash`
  - `encodeGenesisBlock` — the synthetic genesis block; both of the above return it early, before `EncodeTmBlock` is reached
  - `encodeCommittedBlock` — `eth_subscribe("newHeads")` under Autobahn
  - `encodeTmHeader` — `eth_subscribe("newHeads")` under CometBFT
- **Explicitly unsupported RPCs (same `-32000` pattern)** — Methods are registered so clients get a clear error instead of `-32601` method not found:
  - `debug_getRawBlock`, `debug_getRawHeader`, `debug_getRawReceipts`, `debug_getRawTransaction`
  - `eth_newPendingTransactionFilter`
  - `eth_syncing`
  - `eth_getProof` — deprecated rather than permanently incompatible; the message directs callers who need proofs to the Sei team.

## `sei_` prefixed endpoints
The legacy `sei_` namespace contains address and Cosmos transaction helpers.

Legacy **`sei_*`** JSON-RPC (EVM HTTP only) are **gated** by the `[evm].enabled_legacy_sei_apis` list in `app.toml` (after `deny_list`). Enforcement is **centralized** in `wrapSeiLegacyHTTP` (see `sei_legacy_http.go`): it inspects the JSON-RPC `method` field only. Wired from `HTTPServer.EnableRPC` via `HTTPConfig.SeiLegacyAllowlist` — handlers do not duplicate gate logic. The surface is **deprecated** and scheduled for removal; **only methods named in that array** are allowed. `seid init` / `DefaultConfig` and **Docker localnet** (`docker/localnode/config/app.toml`) enable all three remaining address/Cosmos helpers. **HTTP 200** for all responses. **Disabled** methods return JSON-RPC `error` code `-32601`, `message` explains not enabled + deprecated, `data` `"legacy_sei_deprecated"`. **Allowed** single-object bodies pass through **unchanged**; JSON **batches** may be subset-forwarded with responses merged by `id` (for requests that include `id`). Per JSON-RPC 2.0, **notifications** (no `id` in the request) do not produce entries in the batch response array, so the merged array is **not** 1:1 with the request batch when notifications are present; if nothing would be returned, the gateway sends an **empty HTTP body** (not `[]`). Optional deprecation signal: HTTP header `Sei-Legacy-RPC-Deprecation` (`SeiLegacyDeprecationHTTPHeader` in `sei_legacy.go`). Coverage: `evmrpc/sei_legacy_test.go` and `integration_test/evm_module/rpc_io_test/testdata/sei_legacy_deprecation/*.iox`.

## `debug_` prefixed endpoints
`debug_trace*` endpoints should faithfully replay historical execution. If a transaction encountered an error during its actual execution, a `debug_trace*` call for it should reflect so. If a transction consumed X amount of gas during its actual execution, a `debug_trace*` call should show that exact amount as well.

**Tracer gating (deviation from geth defaults):** caller-supplied `TraceConfig.Tracer` values on `debug_traceCall` / `debug_traceTransaction` / `debug_traceBlockBy*` / `debug_traceTransactionProfile` are gated by `[evm]` config in `app.toml`. `trace_allowed_tracers` lists the native geth tracer names callers may request (validated native-only at startup; `muxTracer` nested tracer names are validated recursively with a bounded depth). `trace_allow_js_tracers` (default `false`) is a separate explicit opt-in for request-supplied JavaScript tracer source — upstream geth accepts JS tracers by default, Sei does not. Enabling JS does **not** widen the native allowlist. Validation runs in `validateTraceTracer` (`tracers.go`) before trace-cache lookups and before any tracer is constructed; the default struct logger (no `tracer` field) is always available. `trace_bake_tracers` is held to the same native-only rule at startup.

## Error parity with go-ethereum (`eth_sendRawTransaction`)

Every error leaving `SendAPI.SendRawTransaction` after transaction decoding carries a
go-ethereum message and JSON-RPC code. One package owns that contract:
`evmrpc/ethrpcerrors`.

- `ethrpcerrors.Translate(err)` runs once, at the single exit of `SendAPI.SendRawTransaction`,
  on the error of the `submit` step (proxied call, Cosmos encoding, both broadcast branches).
  Decode errors from `tx.UnmarshalBinary` are go-ethereum's own and are returned untranslated.
- `ethrpcerrors.TranslateABCI(codespace, code, log)` replaces the old
  `sdkerrors.ABCIError(RootCodespace, code, "")`, which discarded the codespace and log and
  rendered `": incorrect account sequence"` / `": unknown"`.
- The returned `*ethrpcerrors.Error` implements `rpc.Error` and `rpc.DataError` and **must be the
  top-level return value**: go-ethereum's `errorMessage` uses `err.(Error)`, so a wrapped one is
  encoded as `-32000` with the wrapper's text. Do not `fmt.Errorf("...: %w", translated)`.
- An error that already implements `rpc.Error`, such as a remote node's `*jsonError` on the
  proxied branch, passes through unchanged.
- Anything the table does not know becomes `-32603 internal error`, increments
  `evmrpc_untranslated_error_total` and logs the original text at Warn. That includes transport
  failures on the proxied branch (`Post "<shard owner url>": …`), so an internal address never
  reaches a client. When the counter moves, add a row to the table rather than widening a match.
- `depguard` (`.golangci.yml`) denies `sei-cosmos/types/errors` under `evmrpc/` except in
  `evmrpc/ethrpcerrors`. It does not cover `_test.go` files (`run.tests: false`) and cannot catch a
  forwarded `Log` string, which is why the golden test in `ethrpcerrors` and the negative
  assertion (no Cosmos vocabulary, no leading `": "`) exist. Extend both when adding a mapping.

**Message format.** The go-ethereum sentinel is the prefix; detail follows after `: `. Producers
in `app/ante` keep their Cosmos SDK error codes and wrap CheckTx-only detail with
`sdkerrors.Wrapf`; the boundary strips the SDK description and prepends the sentinel for the
code. A producer that already emits go-ethereum text passes through verbatim.

**Parity table (submit path).** Codes are `-32000` unless stated.

| Condition | Message | go-ethereum |
|---|---|---|
| Nonce below account nonce | `nonce too low: next nonce N, tx nonce M` | same |
| Same nonce already pending (classic mempool) | `replacement transaction underpriced` | same |
| Nonce gap (autobahn only; classic admits) | `nonce too high: tx nonce N, gapped nonce M` | admitted (see divergences) |
| Insufficient balance | `insufficient funds for gas * price + value: address 0x… have X want Y` | `…: balance X, tx cost Y, overshot Z` (see divergences) |
| Fee cap below base fee | `max fee per gas less than block base fee: address 0x…, maxFeePerGas: X, baseFee: Y` | admitted by the txpool when its tip meets the minimum; execution uses this sentinel |
| Fee cap below Sei minimum fee | `max fee per gas less than block base fee: address 0x…, maxFeePerGas: X, minimumFeePerGas: Y` | no analogue; nearest sentinel, Sei detail |
| Intrinsic gas too low | `intrinsic gas too low: gas N, minimum needed M` | identical |
| Floor data gas too low (EIP-7623, CheckTx only) | `insufficient gas for floor data gas cost: gas N, minimum needed M` | identical |
| Init code exceeds max | `max initcode size exceeded: code size N, limit 49152` | identical |
| Empty EIP-7702 authorization list | `set code tx must have at least one authorization tuple` | identical |
| Unprotected legacy tx | `only replay-protected (EIP-155) transactions allowed over RPC` | identical |
| Gas limit above block max | `exceeds block gas limit: tx gas limit N exceeds block max gas M` | same sentinel |
| Blob tx / type not enabled | `transaction type not supported` | same |
| Chain ID mismatch | `invalid sender: invalid chain id for signer: have N want M` | identical |
| Signature recovery failure | `invalid sender: <reason>` | same |
| Tip above fee cap | `max priority fee per gas higher than max fee per gas (X > Y)` | same sentinel, no detail |
| `cap * gas` or value beyond 2^256-1 | `insufficient funds for gas * price + value: fee out of bound` / `…: value overflow` | no such check; fails the balance check |
| Fee cap / tip beyond 2^256-1 | `max fee per gas higher than 2^256-1` / `max priority fee per gas higher than 2^256-1` | same |
| Already in mempool cache, duplicate | `already known` | same |
| Mempool full (classic or autobahn) | `txpool is full` | same |
| Priority below reservoir cutoff | `transaction underpriced` | same (different mechanism) |
| Tx bytes above mempool limit | `oversized data: <Sei detail>` | same sentinel |
| Request or autobahn wait cancelled, commit wait timed out | `-32002 request timed out` | same |
| Anything else (`not producing`, proxy/RPC-layer internals, transport errors, unknown) | `-32603 internal error` | n/a |

**Deliberate divergences** (documented rather than changed):

- Fee before nonce: `EvmCheckAndChargeFees` runs before `CheckNonce`, so an underfunded account
  replaying a stale nonce gets `insufficient funds…` where go-ethereum says `nonce too low`.
- Sei rejects a fee cap below the current base fee during CheckTx. go-ethereum's legacy txpool
  can retain that transaction for a later base-fee drop when its tip meets the pool minimum.
- Insufficient-funds detail is go-ethereum's execution-path shape (`address … have X want Y`, from
  the fork's `BuyGas`), not the txpool's `balance X, tx cost Y, overshot Z`. The sentinel matches.
- Autobahn requires strictly sequential nonces per sender within a produce session; go-ethereum's
  legacy/1559 pools admit gaps. The classic mempool matches go-ethereum. Which path a client hits
  depends on node configuration.
- `BroadcastTxCommit` is refused under Autobahn; `evm.slow` still submits via `BroadcastTx` there.
- Per-sender pending caps use a priority reservoir and utilisation threshold, not go-ethereum's
  slot count, so `account limit exceeded` is never emitted; overdraft across queued transactions is
  only partially covered by the mempool's required-balance tracking.
- The tip-above-fee-cap detail is parenthesized (`(X > Y)`), produced by `x/evm/types/ethtx`
  validation; the leading sentinel is identical.
- The fallback code is `-32603` where these errors used to be `-32000`; text-matching clients see
  every mapped condition change string, code-matching clients only the fallback.

## Error parity with go-ethereum (block resolution)

A block, receipt set or state version the node cannot serve is one condition rendered several
ways: go-ethereum answers `null` from the endpoints that return a block or something inside one,
and an error from the state-backed ones, and its text differs by endpoint family. The producers
therefore return a typed condition, `*ethrpcerrors.BlockUnavailable`, and each family renders it
at its own entry point. Nothing between the two builds a message.

**Producers.** `WatermarkManager.ResolveHeight`, `EnsureBlockHeightAvailable` and
`EnsureReceiptHeightAvailable`, `blockByNumberWithRetry` / `blockByHashWithRetry`, and
`CheckVersion`. Reasons, selectable with `errors.Is`: `ErrBlockAboveLatest`, `ErrBlockUnknownHash`,
`ErrBlockNotFound` (the block store has nothing at an in-window height), `ErrHistoryPruned` (block or
receipts below the earliest kept height), `ErrStatePruned` (state below the earliest kept height, or
a store with no version at the height). `Detail()` carries the heights for logs and tests; the wire
message does not, because go-ethereum's does not.

**Renderers.**

| Family | Entry point | Rendering |
|---|---|---|
| Block fetch: `eth_getBlockBy*`, `eth_getBlockReceipts`, `eth_getBlockTransactionCountBy*`, `eth_getTransactionByBlock*AndIndex`, `eth_getTransactionByHash`, `eth_getTransactionReceipt` | `blockByNumberOrNullForJSONRPC` / `blockByHashOrNullForJSONRPC` | `IsBlockMissing` → result `null`; pruned → `4444 pruned history unavailable` |
| State: `eth_call`, `eth_estimateGas`, `eth_createAccessList`, `eth_getBalance`, `eth_getCode`, `eth_getStorageAt`, `eth_getTransactionCount` | the condition's own `Error()`; `Backend.StateAndHeaderByNumberOrHash` applies `ForState` | above latest or not in store → `-32000 header not found`; unknown hash → `-32000 header for hash not found`; pruned → `-32000 missing trie node: state at height N is not available[; earliest available is M]` |
| Logs: `eth_getLogs`, `eth_getFilterLogs`, `eth_getFilterChanges` | `ForLogs` in `fetchBlocksByCrit`; `ComputeBlockBounds` and `GetLogs` for the range | missing → `-32000 unknown block`; range below earliest → `4444 pruned history unavailable` |
| Fee history: `eth_feeHistory` | `BeyondHead`, `HistoryPruned` in `FeeHistory` | `-32000 request beyond head block: requested N, head M`; below earliest → `4444 pruned history unavailable` |
| Tracers: `debug_traceBlockBy*`, `debug_traceCall` | `Backend.BlockByNumber` / `BlockByHash` return a nil block when `IsBlockMissing` | go-ethereum's own `block #N not found` / `block 0x… not found`; pruned → `4444 pruned history unavailable` |

`4444 pruned history unavailable` is go-ethereum's `history.PrunedHistoryError` (v1.16, where
history expiry landed); `missing trie node` is the prefix of the trie error its state endpoints
return when the state at a kept header is gone, with Sei's height in place of the node and root
hashes it names. Both keep the sentinel as the prefix and any Sei detail after `: `.

Rules that keep the table true:

- A `*BlockUnavailable` is returned as is. `fmt.Errorf("…: %w", err)` keeps `errors.Is` working
  but reverts the code to `-32000` and prefixes the text (see the send-path typing constraint).
- A new endpoint that takes a block identifier joins one of the families above and goes through
  that family's entry point. A new reason gets a row in `block_test.go`'s golden table.
- `pending`, `safe` and `finalized` resolve to the safe latest height and never produce a
  condition, so go-ethereum's `pending state is not available` and `safe/finalized block not found`
  are never emitted (deliberate; see the distinctions list above).
- Ordering rules the range checks in `ComputeBlockBounds` apply (`fromBlock` above `toBlock`, a
  range past the head) are not availability conditions and keep their own text.

## Consistency
RPC responses for historical heights should never change as the blockchain progresses, or as the blockchain code gets upgraded.

## Exported receivers are RPC surface — treat every export as a new endpoint

`go-ethereum`'s `rpc.Server` registers **every exported method** on a `Service`
struct passed to `RegisterName` (see `evmrpc/server.go`) as a callable JSON-RPC
method, named by lower-casing the method's first letter and prefixing with the
service's namespace (e.g. `InfoAPI.GasPriceHelper` → `eth_gasPriceHelper`).
This applies to `InfoAPI`, `FilterAPI`, `DebugAPI`, and every other struct
registered as a `Service` in `server.go` — there is no separate allowlist step
for "internal" helper methods.

**Any exported method added to a registered API struct is automatically a
live, unaudited RPC endpoint** — with no request validation, no rate-limit
review, and no `sei_*`-style gating unless someone deliberately adds it.

**When reviewing or writing code in `evmrpc/`:** if a change adds, renames, or
un-exports a method on any struct registered via `RegisterName` in
`server.go`, call this out loudly — do not treat it as a routine
rename/refactor. Concretely:

- A new exported method on a registered API struct that is not meant to be a
  public RPC method is a bug, not a style nit. Keep helper/internal methods
  lower-case.
- If a helper genuinely needs to be called from tests outside the package
  (`evmrpc_test`, `evmrpc/tests`), export it via a `*ForTest` wrapper in
  `evmrpc/export_test.go` (see existing examples there) instead of exporting
  the production method itself — `_test.go` files are excluded from
  production builds, so this does not create a real endpoint.
- When reviewing a diff, cross-check any newly-exported method against the
  registered `Service` list in `server.go`; if the receiver type is on that
  list, flag the export explicitly rather than letting it pass as normal Go
  visibility hygiene.
