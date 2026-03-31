# Failed RPC IO analysis (brief)

**Principle:** Fixtures encode **Ethereum-expected behavior**. A test must **fail** when Sei RPC diverges. Fix the **RPC**, not the fixture.

## Runs

### Post-trim baseline (164 fixtures — historical snapshot)

| Metric    | Count |
| --------- | ----- |
| Total     | 164   |
| Passed    | 135   |
| Failed    | 29    |
| Skipped   | 0     |
| Pass rate | 82.3% |

*For the **157**-file explicit-unsupported set on main (without `sei_legacy_deprecation`), see `RPC_IO_README.md` summary column **unsupported-fix** (~142 / ~15 / ~90.4%).*

### With sei_* gating + deprecation IO (159 fixtures)

Docker localnet with expanded `[evm].enabled_legacy_sei_apis` (all gated `sei_*` except `sei_sign`) plus `testdata/sei_legacy_deprecation/*.iox` (two files on top of main's 157).

| Metric    | Count |
| --------- | ----- |
| Total     | 159   |
| Passed    | 145   |
| Failed    | 14    |
| Skipped   | 0     |
| Pass rate | 91.2% |

Reference: `TestEVMRPCSpecSummary` after `evm_rpc_tests.sh` (**159** fixtures, docker localnet with expanded `enabled_legacy_sei_apis`). **Two** `sei_legacy_deprecation/*.iox` pass. **145** pass / **14** fail / **91.2%** (see `RPC_IO_README.md` summary column **sei_* fix**).

**Legacy `sei_*`:** Every **`sei_*`** fixture under `testdata/` passes on that run (blocks, traces, filters, logs, receipts, deprecation `.iox`, etc.). There are **no** `sei2_*` `.io`/`.iox` files in the suite yet.

**Moved from fail to pass** (vs earlier 159-file runs): `eth_getFilterLogs/getFilterLogs-lifecycle.iox` and `sei_getFilterLogs/getFilterLogs.iox` - filter criteria changed from `0x1`->`latest` to `latest`->`latest` so the span stays within `max_blocks_for_log` on long-lived localnet.

**Moved from fail to pass** (baseline to typical docker localnet, varies by build): `eth_blobBaseFee`, `eth_getBlockByHash` (empty / not-found hash), `eth_getBlockReceipts` (empty / not-found hash), `eth_getBlockTransactionCountByHash/get-genesis.iox` - when the node returns spec-shaped `null` or exposes the method.

## Failed tests by endpoint (14 failures on the **159**-file reference run; **unsupported-fix** baseline remains ~15 fails on **157** files without deprecation `.iox`)

`debug_getRaw*`, `eth_newPendingTransactionFilter`, and `eth_syncing` now use **`not-supported.iox`** (expect JSON-RPC error `-32000`); they are not listed below as -32601 failures. See [docs/evm_jsonrpc_unsupported.md](../../../docs/evm_jsonrpc_unsupported.md).

| Endpoint | # | Fixtures / cause |
| -------- | - | ---------------- |
| eth_call | 1 | call-callenv-options-eip1559.iox (EIP1559 params; Sei returns error) |
| eth_createAccessList | 3 | create-al-abi-revert, create-al-contract-eip1559, create-al-contract (insufficient funds / gas fee) |
| eth_estimateGas | 2 | estimate-with-eip4844.iox, estimate-with-eip7702.iox (parse error) |
| eth_estimateGasAfterCalls | 1 | estimateGasAfterCalls.iox (insufficient funds) |
| eth_getBlockByNumber | 1 | get-block-notfound.iox (-32000 e.g. `requested height 1000 is not yet available; safe latest is 128` vs spec `result: null`) |
| eth_getLogs | 1 | filter-error-future-block-range.io (Sei returns []; spec: error when range > head) |
| eth_getProof | 3 | get-account-proof-* (cannot find EVM IAVL store) |
| eth_getTransactionByBlockHashAndIndex | 1 | get-block-n.iox (transaction index out of range) |
| eth_getTransactionByBlockNumberAndIndex | 1 | get-block-n.iox (transaction index out of range) |

## Explicitly unsupported (-32000, documented)

These methods are **implemented** to return JSON-RPC error code `-32000` with a clear message (not `-32601`). Fixtures expect `error`. See [docs/evm_jsonrpc_unsupported.md](../../../docs/evm_jsonrpc_unsupported.md).

| Endpoint | Fixtures |
| -------- | -------- |
| `debug_getRaw*` | `debug_getRawBlock/not-supported.iox`, same for Header/Receipts/Transaction |
| `eth_blobBaseFee` | `eth_blobBaseFee/blobs-not-supported-error.iox` |
| `eth_newPendingTransactionFilter` | `eth_newPendingTransactionFilter/not-supported.iox` |
| `eth_syncing` | `eth_syncing/not-supported.iox` |

`eth_blobBaseFee`: on recent localnet builds the method is often exposed (returns a JSON-RPC error for "blobs not supported" per spec); when missing it failed older runs with -32601.

## Fix direction (no fixture changes)

| Category | Endpoints / fixtures | Action |
| -------- | -------------------- | ------ |
| **Return null for missing block** | eth_getBlockByHash, eth_getBlockReceipts (empty/notfound) | RPC: return `result: null` instead of -32000 for non-existent block hash (if still failing on your node) |
| **Block hash lookup** | eth_getBlockTransactionCountByHash (get-genesis) | RPC: resolve block by hash when that hash was returned by getBlockByNumber |
| **Block range validation** | eth_getLogs (filter-error-future-block-range) | RPC: return -32602 when toBlock > current head |
| **EIP1559 in eth_call** | eth_call (call-callenv-options-eip1559) | RPC: accept maxFeePerGas/maxPriorityFeePerGas and return result |
| **Other** | eth_createAccessList (3), eth_estimateGas (2), eth_estimateGasAfterCalls, eth_getBlockByNumber (notfound), eth_getProof (3), eth_getTransactionBy*Index (2) | Investigate; fix RPC or env (e.g. funded "from", parse, IAVL store, tx index) |

*Removed fixtures (not in suite): call-revert-abi-error.io, call-revert-abi-panic.io, estimate-call-abi-error.io, estimate-failed-call.io. Revert coverage: call-revert-abi-error-sei.iox, call-revert-abi-panic-sei.iox, estimate-call-abi-error-sei.iox, estimate-call-abi-panic-sei.iox (use __REVERTER__). eth_simulateV1 folder is not under testdata.*
