# Failed RPC IO analysis (brief)

**Principle:** Fixtures encode **Ethereum-expected behavior**. A test must **fail** when Sei RPC diverges. Fix the **RPC**, not the fixture.

## Latest run (evm_rpc_tests.sh)

| Metric    | Count |
| --------- | ----- |
| Total     | 164   |
| Passed    | 135   |
| Failed    | 29    |
| Skipped   | 0     |
| Pass rate | 82.3% |

## Failed tests by endpoint (29)

| Endpoint | # | Fixtures / cause |
| -------- | - | ---------------- |
| debug_getRawBlock | 2 | get-block-n.iox, get-genesis.iox |
| debug_getRawHeader | 2 | get-block-n.iox, get-genesis.iox |
| debug_getRawReceipts | 2 | get-block-n.iox, get-genesis.iox |
| debug_getRawTransaction | 1 | get-tx.iox |
| eth_blobBaseFee | 1 | blobs-not-supported-error.iox |
| eth_call | 1 | call-callenv-options-eip1559.iox (EIP1559 params; Sei returns error) |
| eth_createAccessList | 3 | create-al-abi-revert, create-al-contract-eip1559, create-al-contract (insufficient funds / gas fee) |
| eth_estimateGas | 2 | estimate-with-eip4844.iox, estimate-with-eip7702.iox (parse error) |
| eth_estimateGasAfterCalls | 1 | estimateGasAfterCalls.iox (insufficient funds) |
| eth_getBlockByHash | 2 | get-block-by-empty-hash, get-block-by-notfound-hash (Sei returns error; spec: result=null) |
| eth_getBlockByNumber | 1 | get-block-notfound.iox (height not available vs spec null) |
| eth_getBlockReceipts | 2 | get-block-receipts-empty, get-block-receipts-not-found (Sei returns error; spec: result=null) |
| eth_getBlockTransactionCountByHash | 1 | get-genesis.iox (hash lookup: block from getBlockByNumber("0x0") not found by hash) |
| eth_getLogs | 1 | filter-error-future-block-range.io (Sei returns []; spec: error when range > head) |
| eth_getProof | 3 | get-account-proof-* (cannot find EVM IAVL store) |
| eth_getTransactionByBlockHashAndIndex | 1 | get-block-n.iox (transaction index out of range) |
| eth_getTransactionByBlockNumberAndIndex | 1 | get-block-n.iox (transaction index out of range) |
| eth_newPendingTransactionFilter | 1 | newPendingTransactionFilter.iox |
| eth_syncing | 1 | check-syncing.iox |

## RPC not implemented (-32601)

| Endpoint | Fixture(s) | Note |
| -------- | ---------- | ---- |
| debug_getRawBlock | get-block-n, get-genesis | No GetRawBlock on DebugAPI |
| debug_getRawHeader | get-block-n, get-genesis | No GetRawHeader |
| debug_getRawReceipts | get-block-n, get-genesis | No GetRawReceipts |
| debug_getRawTransaction | get-tx.iox | No GetRawTransaction |
| eth_blobBaseFee | blobs-not-supported-error.iox | Not exposed on eth API |
| eth_newPendingTransactionFilter | newPendingTransactionFilter.iox | No NewPendingTransactionFilter in FilterAPI |
| eth_syncing | check-syncing.iox | No Syncing on InfoAPI |

## Fix direction (no fixture changes)

| Category | Endpoints / fixtures | Action |
| -------- | -------------------- | ------ |
| **Return null for missing block** | eth_getBlockByHash, eth_getBlockReceipts (empty/notfound) | RPC: return `result: null` instead of -32000 for non-existent block hash |
| **Block hash lookup** | eth_getBlockTransactionCountByHash (get-genesis) | RPC: resolve block by hash when that hash was returned by getBlockByNumber |
| **Block range validation** | eth_getLogs (filter-error-future-block-range) | RPC: return -32602 when toBlock > current head |
| **EIP1559 in eth_call** | eth_call (call-callenv-options-eip1559) | RPC: accept maxFeePerGas/maxPriorityFeePerGas and return result |
| **Other** | eth_createAccessList (3), eth_estimateGas (2), eth_estimateGasAfterCalls, eth_getBlockByNumber (notfound), eth_getProof (3), eth_getTransactionBy*Index (2) | Investigate; fix RPC or env (e.g. funded “from”, parse, IAVL store, tx index) |

*Removed fixtures (not in suite): call-revert-abi-error.io, call-revert-abi-panic.io, estimate-call-abi-error.io, estimate-failed-call.io. Revert coverage: call-revert-abi-error-sei.iox, call-revert-abi-panic-sei.iox, estimate-call-abi-error-sei.iox, estimate-call-abi-panic-sei.iox (use __REVERTER__). eth_simulateV1 folder is not under testdata.*
