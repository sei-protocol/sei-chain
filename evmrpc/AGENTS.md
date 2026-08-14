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
- **Explicitly unsupported RPCs (same `-32000` pattern)** — Methods are registered so clients get a clear error instead of `-32601` method not found:
  - `debug_getRawBlock`, `debug_getRawHeader`, `debug_getRawReceipts`, `debug_getRawTransaction`
  - `eth_newPendingTransactionFilter`
  - `eth_syncing`

## `sei_` prefixed endpoints
The legacy `sei_` namespace contains address/Cosmos transaction helpers and a synthetic-aware transaction receipt method.

Legacy **`sei_*`** JSON-RPC (EVM HTTP only) are **gated** by the `[evm].enabled_legacy_sei_apis` list in `app.toml` (after `deny_list`). Enforcement is **centralized** in `wrapSeiLegacyHTTP` (see `sei_legacy_http.go`): it inspects the JSON-RPC `method` field only. Wired from `HTTPServer.EnableRPC` via `HTTPConfig.SeiLegacyAllowlist` — handlers do not duplicate gate logic. The surface is **deprecated** and scheduled for removal; **only methods named in that array** are allowed. `seid init` / `DefaultConfig` pre-fill the three `sei_*` address/Cosmos helpers; the receipt method appears **commented** in the generated template. **Docker localnet** (`docker/localnode/config/app.toml`) enables **all remaining** gated methods. **HTTP 200** for all responses. **Disabled** methods return JSON-RPC `error` code `-32601`, `message` explains not enabled + deprecated, `data` `"legacy_sei_deprecated"`. **Allowed** single-object bodies pass through **unchanged**; JSON **batches** may be subset-forwarded with responses merged by `id` (for requests that include `id`). Per JSON-RPC 2.0, **notifications** (no `id` in the request) do not produce entries in the batch response array, so the merged array is **not** 1:1 with the request batch when notifications are present; if nothing would be returned, the gateway sends an **empty HTTP body** (not `[]`). Optional deprecation signal: HTTP header `Sei-Legacy-RPC-Deprecation` (`SeiLegacyDeprecationHTTPHeader` in `sei_legacy.go`). Coverage: `evmrpc/sei_legacy_test.go` and `integration_test/evm_module/rpc_io_test/testdata/sei_legacy_deprecation/*.iox`.

## `debug_` prefixed endpoints
`debug_trace*` endpoints should faithfully replay historical execution. If a transaction encountered an error during its actual execution, a `debug_trace*` call for it should reflect so. If a transction consumed X amount of gas during its actual execution, a `debug_trace*` call should show that exact amount as well.

**Tracer gating (deviation from geth defaults):** caller-supplied `TraceConfig.Tracer` values on `debug_traceCall` / `debug_traceTransaction` / `debug_traceBlockBy*` / `debug_traceTransactionProfile` are gated by `[evm]` config in `app.toml`. `trace_allowed_tracers` lists the native geth tracer names callers may request (validated native-only at startup; `muxTracer` nested tracer names are validated recursively with a bounded depth). `trace_allow_js_tracers` (default `false`) is a separate explicit opt-in for request-supplied JavaScript tracer source — upstream geth accepts JS tracers by default, Sei does not. Enabling JS does **not** widen the native allowlist. Validation runs in `validateTraceTracer` (`tracers.go`) before trace-cache lookups and before any tracer is constructed; the default struct logger (no `tracer` field) is always available. `trace_bake_tracers` is held to the same native-only rule at startup.

## Consistency
RPC responses for historical heights should never change as the blockchain progresses, or as the blockchain code gets upgraded.
