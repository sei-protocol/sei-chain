# EVM RPC .io / .iox tests

Integration tests for Sei EVM RPC compatibility with Ethereum JSON-RPC. The suite runs **162 tests** from `testdata/` against a live RPC endpoint.

## How to run

1. Start the local cluster: `make docker-cluster-start` (EVM RPC on port 8545).
2. Run the script from repo root:
  ```bash
   ./integration_test/evm_module/scripts/evm_rpc_tests.sh
  ```

When the target is localhost, the script sends one EVM tx and deploys one contract inside the node container before `go test`, so data-dependent `.iox` tests have block/tx/contract. Default RPC URL: `http://127.0.0.1:8545` (override with `SEI_EVM_RPC_URL`).

## Test mix


| Kind      | Count | Description                                                                                                                              |
| --------- | ----- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| **.io**   | ~50   | Request/response fixtures; curated from [ethereum/execution-apis](https://github.com/ethereum/execution-apis) plus Sei-added.            |
| **.iox**  | ~112  | Sei-generated; use `@ bind` and optional `@ ref_pair N` so data comes from a first request. |
| **Total** | 162   | All under `testdata/`; runner executes every .io and .iox file.                                                                          |


Fixtures live in `testdata/`; see `testdata/README.md` (do not overwrite with a raw copy from execution-apis).

### Removed tests

The following fixtures were **removed** (no longer in the suite) because they depended on execution-apis testnet state (fixed contract addresses that do not exist on Sei). Revert/estimateGas behavior is covered by self-contained Sei fixtures instead.

| Removed fixture | Reason | Replacement |
| ----------------- | ------ | ----------- |
| `eth_call/call-revert-abi-error.io` | Fixed address `0x0ee3ab...` (reverting contract on execution-apis only) | `eth_call/call-revert-abi-error-sei.iox` (uses `__REVERTER__`, script-deployed) |
| `eth_call/call-revert-abi-panic.io` | Same fixed address, panic case | No replacement; Error(string) revert covered by above |
| `eth_estimateGas/estimate-call-abi-error.io` | Same fixed address, expects revert error | `eth_estimateGas/estimate-call-abi-error-sei.iox` (uses `__REVERTER__`) |
| `eth_estimateGas/estimate-failed-call.io` | Fixed address `0x17e7ee...`, expects revert error | Revert case covered by `estimate-call-abi-error-sei.iox` |

The total count (162) reflects the current `.io`/`.iox` set under `testdata/` as of the latest run.

## What is checked

**Spec-only:** For each request/response pair, the runner only checks that the response *kind* matches the expected one: presence of `result` vs `error`. Response values are not compared.

## Outcomes

- **Pass** - Response kind matches expected.
- **Skip** - A required binding is missing (e.g. `${txHash}`, `${deployTxHash}`). Typical cause: latest block has no transactions when the test runs. Data dependency.
- **Fail** - Response kind mismatch: node returned `result` when test expects `error`, or `error` when test expects `result`. On each failure the runner logs **actual response**: the node's `error.code` and `error.message` (or a short `result` snippet). Use that to tell **not implemented** (e.g. code -32601), **invalid params** (-32602), **disabled endpoint**, or other spec mismatch.

### What "seed" means here

**Seed** = the block we create before tests run (by sending a deploy tx in the script) so that data-dependent fixtures have deterministic data to query.

1. The script sends one EVM tx and deploys one contract; the **deploy block** is the block that includes that deploy.
2. The script sets `SEI_EVM_IO_SEED_BLOCK` to that block number (hex) and `SEI_EVM_IO_DEPLOY_TX_HASH` to the deploy tx hash.
3. In `.iox` fixtures, `__SEED__` in a request is replaced by that block number (or by `"latest"` if the script didn't run / seed isn't set).
4. Fixtures can bind `${txHash}` from the first request (e.g. `eth_getBlockByNumber(__SEED__, true)` -> `result.transactions.0.hash`) and `${deployTxHash}` is pre-filled from the script when set, so later requests use a known block and known tx hashes instead of "latest" (which might be empty).
5. The script also deploys a **reverter** contract (reverts with `Error("user error")`); it sets `SEI_EVM_IO_REVERTER_ADDRESS`. In fixtures, `__REVERTER__` in a request is replaced by that address. Used by `eth_call/call-revert-abi-error-sei.iox`. If a fixture uses `__REVERTER__` and the env is not set, the test is skipped.

So "seed" = a known-good block (and deploy tx) that the script creates and the runner uses so **SEED** and deploy/tx bindings resolve.

---

## Test results (latest run)

*Source:* **Eth exec api** = from [ethereum/execution-apis](https://github.com/ethereum/execution-apis) (`.io`); **Sei** = Sei-generated (`.iox` or Sei-added `.io`).

### Passed tests (133)


| Endpoint                               | Test                                                           | Source       |
| -------------------------------------- | -------------------------------------------------------------- | ------------ |
| cross_check                            | get-block-by-number-then-by-hash.iox                           | Sei          |
| debug_getRawBlock                      | get-invalid-number.io                                          | Eth exec api |
| debug_getRawHeader                     | get-invalid-number.io                                          | Eth exec api |
| debug_getRawReceipts                   | get-invalid-number.io                                          | Eth exec api |
| debug_getRawTransaction                | get-invalid-hash.io                                            | Eth exec api |
| debug_traceBlockByHash                 | traceBlockByHash.iox                                           | Sei          |
| debug_traceBlockByNumber               | traceBlockByNumber.iox                                         | Sei          |
| debug_traceBlockByNumber               | traceBlockByNumber-latest.io                                   | Eth exec api |
| debug_traceCall                        | traceCall.io                                                   | Eth exec api |
| debug_traceStateAccess                 | traceStateAccess-not-found.io                                  | Eth exec api |
| debug_traceStateAccess                 | traceStateAccess.iox                                           | Sei          |
| debug_traceTransaction                 | traceTransaction-not-found.io                                  | Eth exec api |
| debug_traceTransaction                 | traceTransaction.iox                                           | Sei          |
| echo_echo                              | echo.io                                                        | Sei          |
| eth_accounts                           | accounts.io                                                    | Sei          |
| eth_blockNumber                        | simple-test.io                                                 | Eth exec api |
| eth_call                               | call-callenv.io                                                | Eth exec api |
| eth_call                               | call-contract-from-deploy.iox                                  | Sei          |
| eth_call                               | call-contract.io                                               | Eth exec api |
| eth_call                               | call-eip7702-delegation.io                                     | Eth exec api |
| eth_call                               | call-revert-abi-error-sei.iox                                  | Sei          |
| eth_chainId                            | get-chain-id.io                                                | Eth exec api |
| eth_coinbase                           | coinbase.io                                                    | Sei          |
| eth_createAccessList                   | create-al-value-transfer.iox                                   | Sei          |
| eth_estimateGas                        | estimate-call-abi-error-sei.iox                                | Sei          |
| eth_estimateGas                        | estimate-gas-from-deploy.iox                                   | Sei          |
| eth_estimateGas                        | estimate-simple-transfer.io                                    | Eth exec api |
| eth_estimateGas                        | estimate-successful-call.io                                    | Eth exec api |
| eth_feeHistory                         | fee-history.io                                                 | Eth exec api |
| eth_gasPrice                           | gasPrice.io                                                    | Sei          |
| eth_getBalance                         | get-balance-blockhash.iox                                      | Sei          |
| eth_getBalance                         | get-balance-unknown-account.io                                 | Eth exec api |
| eth_getBalance                         | get-balance.io                                                 | Eth exec api |
| eth_getBlockByHash                     | get-block-by-hash.iox                                          | Sei          |
| eth_getBlockByNumber                   | get-block-cancun-fork.io                                       | Eth exec api |
| eth_getBlockByNumber                   | get-block-london-fork.io                                       | Eth exec api |
| eth_getBlockByNumber                   | get-block-merge-fork.io                                        | Eth exec api |
| eth_getBlockByNumber                   | get-block-prague-fork.io                                       | Eth exec api |
| eth_getBlockByNumber                   | get-block-shanghai-fork.io                                     | Eth exec api |
| eth_getBlockByNumber                   | get-finalized.io                                               | Eth exec api |
| eth_getBlockByNumber                   | get-genesis.io                                                 | Eth exec api |
| eth_getBlockByNumber                   | get-latest-full-then-by-hash.iox                               | Sei          |
| eth_getBlockByNumber                   | get-latest.io                                                  | Eth exec api |
| eth_getBlockByNumber                   | get-safe.io                                                    | Eth exec api |
| eth_getBlockReceipts                   | get-block-receipts-0.io                                        | Eth exec api |
| eth_getBlockReceipts                   | get-block-receipts-by-hash.iox                                 | Sei          |
| eth_getBlockReceipts                   | get-block-receipts-earliest.io                                 | Eth exec api |
| eth_getBlockReceipts                   | get-block-receipts-future.io                                   | Eth exec api |
| eth_getBlockReceipts                   | get-block-receipts-latest.io                                   | Eth exec api |
| eth_getBlockReceipts                   | get-block-receipts-n.io                                        | Eth exec api |
| eth_getBlockReceipts                   | get-receipts-by-latest-block.iox                               | Sei          |
| eth_getBlockTransactionCountByHash     | get-block-n.iox                                                | Sei          |
| eth_getBlockTransactionCountByNumber   | get-block-n.io                                                 | Eth exec api |
| eth_getBlockTransactionCountByNumber   | get-genesis.io                                                 | Eth exec api |
| eth_getCode                            | get-code-eip7702-delegation.io                                 | Eth exec api |
| eth_getCode                            | get-code-from-deploy.iox                                       | Sei          |
| eth_getCode                            | get-code-unknown-account.io                                    | Eth exec api |
| eth_getCode                            | get-code.io                                                    | Eth exec api |
| eth_getFilterChanges                   | getFilterChanges-invalid-id.io                                 | Eth exec api |
| eth_getFilterChanges                   | getFilterChanges-lifecycle.iox                                 | Sei          |
| eth_getFilterLogs                      | getFilterLogs-invalid-id.io                                    | Eth exec api |
| eth_getFilterLogs                      | getFilterLogs-lifecycle.iox                                    | Sei          |
| eth_getLogs                            | contract-addr.io                                               | Eth exec api |
| eth_getLogs                            | filter-error-invalid-blockHash-and-range.io                    | Eth exec api |
| eth_getLogs                            | filter-error-reversed-block-range.io                           | Eth exec api |
| eth_getLogs                            | filter-with-blockHash-and-topics.io                            | Eth exec api |
| eth_getLogs                            | filter-with-blockHash.io                                       | Eth exec api |
| eth_getLogs                            | no-topics.io                                                   | Eth exec api |
| eth_getLogs                            | topic-exact-match.io                                           | Eth exec api |
| eth_getLogs                            | topic-wildcard.io                                              | Eth exec api |
| eth_getStorageAt                       | get-storage-invalid-key-too-large.io                           | Eth exec api |
| eth_getStorageAt                       | get-storage-invalid-key.io                                     | Eth exec api |
| eth_getStorageAt                       | get-storage-unknown-account.io                                 | Eth exec api |
| eth_getStorageAt                       | get-storage.io                                                 | Eth exec api |
| eth_getTransactionByHash               | get-access-list.io                                             | Eth exec api |
| eth_getTransactionByHash               | get-blob-tx.io                                                 | Eth exec api |
| eth_getTransactionByHash               | get-dynamic-fee.io                                             | Eth exec api |
| eth_getTransactionByHash               | get-empty-tx.io                                                | Eth exec api |
| eth_getTransactionByHash               | get-legacy-create.io                                           | Eth exec api |
| eth_getTransactionByHash               | get-legacy-input.io                                            | Eth exec api |
| eth_getTransactionByHash               | get-legacy-tx.io                                               | Eth exec api |
| eth_getTransactionByHash               | get-notfound-tx.io                                             | Eth exec api |
| eth_getTransactionByHash               | get-setcode-tx.io                                              | Eth exec api |
| eth_getTransactionByHash               | get-tx-from-latest-block.iox                                   | Sei          |
| eth_getTransactionCount                | get-nonce-eip7702-account.io                                   | Eth exec api |
| eth_getTransactionCount                | get-nonce-unknown-account.io                                   | Eth exec api |
| eth_getTransactionCount                | get-nonce.io                                                   | Eth exec api |
| eth_getTransactionErrorByHash          | getTransactionErrorByHash-not-found.io                         | Sei          |
| eth_getTransactionErrorByHash          | getTransactionErrorByHash.io                                   | Sei          |
| eth_getTransactionReceipt              | get-access-list.io                                             | Eth exec api |
| eth_getTransactionReceipt              | get-blob-tx.io                                                 | Eth exec api |
| eth_getTransactionReceipt              | get-dynamic-fee.io                                             | Eth exec api |
| eth_getTransactionReceipt              | get-empty-tx.io                                                | Eth exec api |
| eth_getTransactionReceipt              | get-legacy-contract.io                                         | Eth exec api |
| eth_getTransactionReceipt              | get-legacy-input.io                                            | Eth exec api |
| eth_getTransactionReceipt              | get-legacy-receipt.io                                          | Eth exec api |
| eth_getTransactionReceipt              | get-notfound-tx.io                                             | Eth exec api |
| eth_getTransactionReceipt              | get-receipt-from-latest-block.iox                              | Sei          |
| eth_getTransactionReceipt              | get-setcode-tx.io                                              | Eth exec api |
| eth_getVMError                         | getVMError-not-found.io                                        | Sei          |
| eth_getVMError                         | getVMError.iox                                                 | Sei          |
| eth_maxPriorityFeePerGas               | maxPriorityFeePerGas.io                                        | Sei          |
| eth_newBlockFilter                     | newBlockFilter.io                                              | Sei          |
| eth_newFilter                          | newFilter.io                                                   | Sei          |
| eth_sendRawTransaction                 | send-access-list-transaction.iox                               | Sei          |
| eth_sendRawTransaction                 | send-blob-tx.iox                                               | Sei          |
| eth_sendRawTransaction                 | send-dynamic-fee-access-list-transaction.iox                   | Sei          |
| eth_sendRawTransaction                 | send-dynamic-fee-transaction.iox                               | Sei          |
| eth_sendRawTransaction                 | send-legacy-transaction.iox                                    | Sei          |
| eth_sendTransaction                    | sendTransaction-unsupported.io                                 | Sei          |
| eth_sign                               | sign-unsupported.io                                            | Sei          |
| eth_signTransaction                    | signTransaction-unsupported.io                                 | Sei          |
| eth_uninstallFilter                    | uninstallFilter-invalid-id.io                                  | Eth exec api |
| eth_uninstallFilter                    | uninstallFilter-lifecycle.io                                   | Eth exec api |
| net_version                            | get-network-id.io                                              | Eth exec api |
| sei_associate                          | associate-invalid.io                                           | Sei          |
| sei_getBlockByHashExcludeTraceFail     | getBlockByHashExcludeTraceFail.iox                             | Sei          |
| sei_getBlockByNumberExcludeTraceFail   | getBlockByNumberExcludeTraceFail.io                            | Sei          |
| sei_getCosmosTx                        | getCosmosTx.io                                                 | Sei          |
| sei_getEVMAddress                      | getEVMAddress-invalid.io                                       | Sei          |
| sei_getEvmTx                           | getEvmTx-invalid.io                                            | Sei          |
| sei_getFilterChanges                   | getFilterChanges.iox                                           | Sei          |
| sei_getFilterLogs                      | getFilterLogs.iox                                              | Sei          |
| sei_getLogs                            | getLogs.io                                                     | Sei          |
| sei_getSeiAddress                      | getSeiAddress-not-found.io                                     | Sei          |
| sei_getTransactionReceiptExcludeTraceFail | getTransactionReceiptExcludeTraceFail.iox                   | Sei          |
| sei_newBlockFilter                     | newBlockFilter.io                                              | Sei          |
| sei_newFilter                          | newFilter.io                                                   | Sei          |
| sei_traceBlockByHashExcludeTraceFail   | traceBlockByHashExcludeTraceFail.iox                           | Sei          |
| sei_traceBlockByNumberExcludeTraceFail | traceBlockByNumberExcludeTraceFail.iox                         | Sei          |
| sei_uninstallFilter                    | uninstallFilter.io                                             | Sei          |
| txpool_content                         | content.io                                                     | Sei          |
| web3_clientVersion                     | clientVersion.io                                               | Sei          |


### Failed tests (29)


| Endpoint                           | Test                                                                              | Status | Source       | Reason                 | Error message                                                                            |
| ---------------------------------- | --------------------------------------------------------------------------------- | ------ | ------------ | ---------------------- | ---------------------------------------------------------------------------------------- |
| debug_getRawBlock                  | get-block-n.iox                                                                   | FAIL   | Sei          | Not implemented        | error code=-32601 message="the method debug_getRawBlock does not exist/is not available" |
| debug_getRawBlock                  | get-genesis.iox                                                                   | FAIL   | Sei          | Not implemented        | error code=-32601 message="the method debug_getRawBlock does not exist/is not available" |
| debug_getRawHeader                 | get-block-n.iox                                                                   | FAIL   | Sei          | Not implemented        | error code=-32601 message="the method debug_getRawHeader does not exist/is not available" |
| debug_getRawHeader                 | get-genesis.iox                                                                   | FAIL   | Sei          | Not implemented        | error code=-32601 message="the method debug_getRawHeader does not exist/is not available" |
| debug_getRawReceipts               | get-block-n.iox                                                                   | FAIL   | Sei          | Not implemented        | error code=-32601 message="the method debug_getRawReceipts does not exist/is not available" |
| debug_getRawReceipts               | get-genesis.iox                                                                   | FAIL   | Sei          | Not implemented        | error code=-32601 message="the method debug_getRawReceipts does not exist/is not available" |
| debug_getRawTransaction            | get-tx.iox                                                                        | FAIL   | Sei          | Not implemented        | error code=-32601 message="the method debug_getRawTransaction does not exist/is not available" |
| eth_blobBaseFee                    | get-current-blobfee.iox                                                           | FAIL   | Sei          | Not implemented        | error code=-32601 message="the method eth_blobBaseFee does not exist/is not available" |
| eth_call                           | call-callenv-options-eip1559.iox                                                  | FAIL   | Sei          | Gas fee issue          | error code=-32000 message="max fee per gas less than block base fee" |
| eth_createAccessList               | create-al-abi-revert.iox                                                          | FAIL   | Sei          | Insufficient funds     | error code=-32000 message="insufficient funds for gas * price + value" |
| eth_createAccessList               | create-al-contract-eip1559.iox                                                    | FAIL   | Sei          | Gas fee issue          | error code=-32000 message="max fee per gas less than block base fee" |
| eth_createAccessList               | create-al-contract.iox                                                             | FAIL   | Sei          | Insufficient funds     | error code=-32000 message="insufficient funds for gas * price + value" |
| eth_estimateGas                    | estimate-with-eip4844.iox                                                         | FAIL   | Sei          | Parse error            | error code=-32700 message="parse error" |
| eth_estimateGas                    | estimate-with-eip7702.iox                                                         | FAIL   | Sei          | Parse error            | error code=-32700 message="parse error" |
| eth_estimateGasAfterCalls          | estimateGasAfterCalls.iox                                                         | FAIL   | Sei          | Insufficient funds     | error code=-32000 message="insufficient funds for gas * price + value" |
| eth_getBlockByHash                 | get-block-by-empty-hash.iox                                                       | FAIL   | Sei          | Block not found        | error code=-32000 message="could not find block for hash 0000000000000000000000000000000000000000000000000000000000000000" |
| eth_getBlockByHash                 | get-block-by-notfound-hash.iox                                                    | FAIL   | Sei          | Block not found        | error code=-32000 message="could not find block for hash 00000000000000000000000000000000000000000000DEADBEEF" |
| eth_getBlockByNumber               | get-block-notfound.iox                                                            | FAIL   | Sei          | Block not available    | error code=-32000 message="requested height 1000 is not yet available; safe latest is 47" |
| eth_getBlockReceipts               | get-block-receipts-empty.iox                                                      | FAIL   | Sei          | Block not found        | error code=-32000 message="could not find block for hash 0000000000000000000000000000000000000000000000000000000000000000" |
| eth_getBlockReceipts               | get-block-receipts-not-found.iox                                                  | FAIL   | Sei          | Block not found        | error code=-32000 message="could not find block for hash 00000000000000000000000000000000000000000000DEADBEEF" |
| eth_getBlockTransactionCountByHash | get-genesis.iox                                                                   | FAIL   | Sei          | Block not found        | error code=-32000 message="could not find block for hash F9D3845DF25B43B1C6926F3CEDA6845C17F5624E12212FD8847D0BA01DA1AB9E" |
| eth_getLogs                        | filter-error-future-block-range.io                                                | FAIL   | Eth exec api | Expected error, got result | response kind mismatch: expected result=false error=true, actual result=true error=false |
| eth_getProof                       | get-account-proof-blockhash.iox                                                   | FAIL   | Sei          | Store not found        | error code=-32000 message="cannot find EVM IAVL store" |
| eth_getProof                       | get-account-proof-latest.iox                                                      | FAIL   | Sei          | Store not found        | error code=-32000 message="cannot find EVM IAVL store" |
| eth_getProof                       | get-account-proof-with-storage.iox                                                | FAIL   | Sei          | Store not found        | error code=-32000 message="cannot find EVM IAVL store" |
| eth_getTransactionByBlockHashAndIndex | get-block-n.iox                                                                | FAIL   | Sei          | Index out of range     | error code=-32000 message="transaction index out of range" |
| eth_getTransactionByBlockNumberAndIndex | get-block-n.iox                                                            | FAIL   | Sei          | Index out of range     | error code=-32000 message="transaction index out of range" |
| eth_newPendingTransactionFilter    | newPendingTransactionFilter.iox                                                   | FAIL   | Sei          | Not implemented        | error code=-32601 message="the method eth_newPendingTransactionFilter does not exist/is not available" |
| eth_syncing                        | check-syncing.iox                                                                 | FAIL   | Sei          | Not implemented        | error code=-32601 message="the method eth_syncing does not exist/is not available" |


### Skipped tests (0)

With the script setting **SEI_EVM_IO_SEED_BLOCK** and **SEI_EVM_IO_DEPLOY_TX_HASH**, no tests are skipped in the latest run. If you run `go test` without the script, some tests may skip for missing `${txHash}` or `${deployTxHash}`. When a test skips, the runner logs **[SKIP]** lines with bindings and placeholders so you can see why.

**Debug one or a few SEED tests:** Run only specific files with extra per-pair logging (request after substitution, bindings, whether `result.transactions` is present):

```bash
SEI_EVM_IO_RUN_INTEGRATION=1 SEI_EVM_IO_DEBUG_FILES="debug_getRawTransaction/get-tx.iox" go test ./integration_test/evm_module/rpc_io_test/ -v -run TestEVMRPCSpec
```

Use a comma-separated list to run up to a few files, e.g. `debug_getRawTransaction/get-tx.iox,debug_traceTransaction/traceTransaction.iox`. Logs show `SEI_EVM_IO_SEED_BLOCK`, each pair's placeholders and binding values, the actual request sent, and bindings after each response.

### Summary


| Metric          | Count |
| --------------- | ----- |
| **Total tests** | 162   |
| **Passed**      | 133   |
| **Failed**      | 29    |
| **Skipped**     | 0     |
| **Pass rate**   | 82.1% |



| Metric                               | Count                                                                                                                      |
| ------------------------------------ | -------------------------------------------------------------------------------------------------------------------------- |
| **Total endpoints tested**           | 70                                                                                                                          |
| **Endpoints with at least one pass** | ~60                                                                                                                         |
| **Missing / untested endpoints**    | None in this suite. Every method folder under `testdata/` is exercised; skips and failures are per-test, not per-endpoint. |

**Why 71 → 70 endpoints?** Earlier documentation sometimes referred to **71** endpoints. The difference is **eth_simulateV1**: that folder (1 endpoint, 64 fixtures) is no longer under `testdata/`—it was removed or is tracked separately as a Sei-specific extension—so the current suite has **70** endpoint folders. The previous "~60" in this table was an underestimate; the actual count is **70**.


*Results are from a single local run; re-run `evm_rpc_tests.sh` to refresh.*
