# EVM RPC .io / .iox tests

Integration tests for Sei EVM RPC compatibility with Ethereum JSON-RPC. The suite runs **255 tests** (105 `.io` + 150 `.iox`) from `testdata/` against a live RPC endpoint.

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
| **.io**   | 105   | Request/response fixtures; curated from [ethereum/execution-apis](https://github.com/ethereum/execution-apis) plus Sei-added.            |
| **.iox**  | 150   | Sei-generated; use `@ bind` (and optional `@ expect_same_block`) so data comes from a first request (e.g. latest block, deploy receipt). |
| **Total** | 255   | All under `testdata/`; runner executes every .io and .iox file.                                                                          |


Fixtures live in `testdata/`; see `testdata/README.md` (do not overwrite with a raw copy from execution-apis).

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

So "seed" = a known-good block (and deploy tx) that the script creates and the runner uses so **SEED** and deploy/tx bindings resolve.

---

## Test results (latest run)

*Source:* **Eth exec api** = from [ethereum/execution-apis](https://github.com/ethereum/execution-apis) (`.io`); **Sei** = Sei-generated (`.iox` or Sei-added `.io`).

### Passed tests (157)


| Endpoint                               | Test                                                           | Source       |
| -------------------------------------- | -------------------------------------------------------------- | ------------ |
| cross_check                            | get-block-by-number-then-by-hash.iox                           | Sei          |
| debug_getRawBlock                      | get-invalid-number.io                                          | Eth exec api |
| debug_getRawHeader                     | get-invalid-number.io                                          | Eth exec api |
| debug_getRawReceipts                   | get-invalid-number.io                                          | Eth exec api |
| debug_getRawTransaction                | get-invalid-hash.io                                            | Eth exec api |
| debug_traceBlockByHash                 | traceBlockByHash.iox                                           | Sei          |
| debug_traceBlockByNumber               | traceBlockByNumber.iox                                         | Sei          |
| debug_traceCall                        | traceCall.io                                                   | Eth exec api |
| debug_traceStateAccess                 | traceStateAccess-not-found.io                                  | Eth exec api |
| debug_traceTransaction                 | traceTransaction-not-found.io                                  | Eth exec api |
| echo_echo                              | echo.io                                                        | Sei          |
| eth_accounts                           | accounts.io                                                    | Sei          |
| eth_blockNumber                        | simple-test.io                                                 | Eth exec api |
| eth_call                               | call-callenv.io                                                | Eth exec api |
| eth_call                               | call-contract-from-deploy.iox                                  | Sei          |
| eth_call                               | call-contract.io                                               | Eth exec api |
| eth_call                               | call-eip7702-delegation.io                                     | Eth exec api |
| eth_chainId                            | get-chain-id.io                                                | Eth exec api |
| eth_coinbase                           | coinbase.io                                                    | Sei          |
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
| eth_getTransactionReceipt              | get-setcode-tx.io                                              | Eth exec api |
| eth_getVMError                         | getVMError-not-found.io                                        | Sei          |
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
| eth_simulateV1                         | ethSimulate-add-more-non-defined-BlockStateCalls-than-fit.iox  | Sei          |
| eth_simulateV1                         | ethSimulate-basefee-too-low-with-validation-38012.iox          | Sei          |
| eth_simulateV1                         | ethSimulate-big-block-state-calls-array.iox                    | Sei          |
| eth_simulateV1                         | ethSimulate-block-num-order-38020.iox                          | Sei          |
| eth_simulateV1                         | ethSimulate-block-override-reflected-in-contract-simple.iox    | Sei          |
| eth_simulateV1                         | ethSimulate-block-override-reflected-in-contract.iox           | Sei          |
| eth_simulateV1                         | ethSimulate-block-timestamp-auto-increment.iox                 | Sei          |
| eth_simulateV1                         | ethSimulate-block-timestamp-non-increment.iox                  | Sei          |
| eth_simulateV1                         | ethSimulate-block-timestamp-order-38021.iox                    | Sei          |
| eth_simulateV1                         | ethSimulate-check-invalid-nonce.iox                            | Sei          |
| eth_simulateV1                         | ethSimulate-empty-with-block-num-set-plus1.iox                 | Sei          |
| eth_simulateV1                         | ethSimulate-gas-fees-and-value-error-38014-with-validation.iox | Sei          |
| eth_simulateV1                         | ethSimulate-gas-fees-and-value-error-38014.iox                 | Sei          |
| eth_simulateV1                         | ethSimulate-instrict-gas-38013.iox                             | Sei          |
| eth_simulateV1                         | ethSimulate-make-call-with-future-block.iox                    | Sei          |
| eth_simulateV1                         | ethSimulate-move-to-address-itself-reference-38022.iox         | Sei          |
| eth_simulateV1                         | ethSimulate-move-two-non-precompiles-accounts-to-same.iox      | Sei          |
| eth_simulateV1                         | ethSimulate-overflow-nonce-validation.iox                      | Sei          |
| eth_simulateV1                         | ethSimulate-override-address-twice.iox                         | Sei          |
| eth_simulateV1                         | ethSimulate-override-all-in-BlockStateCalls.iox                | Sei          |
| eth_simulateV1                         | ethSimulate-simple-no-funds-with-balance-querying.iox          | Sei          |
| eth_simulateV1                         | ethSimulate-simple-no-funds-with-validation-without-nonces.iox | Sei          |
| eth_simulateV1                         | ethSimulate-simple-no-funds-with-validation.iox                | Sei          |
| eth_simulateV1                         | ethSimulate-simple-no-funds.iox                                | Sei          |
| eth_simulateV1                         | ethSimulate-simple-send-from-contract-no-balance.iox           | Sei          |
| eth_simulateV1                         | ethSimulate-simple-send-from-contract-with-validation.iox      | Sei          |
| eth_simulateV1                         | ethSimulate-try-to-move-non-precompile.iox                     | Sei          |
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
| sei_getLogs                            | getLogs.io                                                     | Sei          |
| sei_getSeiAddress                      | getSeiAddress-not-found.io                                     | Sei          |
| sei_newBlockFilter                     | newBlockFilter.io                                              | Sei          |
| sei_newFilter                          | newFilter.io                                                   | Sei          |
| sei_traceBlockByHashExcludeTraceFail   | traceBlockByHashExcludeTraceFail.iox                           | Sei          |
| sei_traceBlockByNumberExcludeTraceFail | traceBlockByNumberExcludeTraceFail.iox                         | Sei          |
| sei_uninstallFilter                    | uninstallFilter.io                                             | Sei          |
| txpool_content                         | content.io                                                     | Sei          |
| web3_clientVersion                     | clientVersion.io                                               | Sei          |


### Failed tests (98)


| Endpoint                           | Test                                                                              | Status | Source       | Reason                 | Error message                                                                            |
| ---------------------------------- | --------------------------------------------------------------------------------- | ------ | ------------ | ---------------------- | ---------------------------------------------------------------------------------------- |
| debug_getRawBlock                  | get-block-n.iox                                                                   | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| debug_getRawBlock                  | get-genesis.iox                                                                   | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| debug_getRawHeader                 | get-block-n.iox                                                                   | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| debug_getRawHeader                 | get-genesis.iox                                                                   | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| debug_getRawReceipts               | get-block-n.iox                                                                   | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| debug_getRawReceipts               | get-genesis.iox                                                                   | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_blobBaseFee                    | get-current-blobfee.iox                                                           | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_call                           | call-revert-abi-error.io                                                          | FAIL   | Eth exec api | Spec-only check failed | response kind mismatch: expected result=false error=true, actual result=true error=false |
| eth_call                           | call-revert-abi-panic.io                                                          | FAIL   | Eth exec api | Spec-only check failed | response kind mismatch: expected result=false error=true, actual result=true error=false |
| eth_createAccessList               | create-al-value-transfer.iox                                                      | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_estimateGas                    | estimate-call-abi-error.io                                                        | FAIL   | Eth exec api | Spec-only check failed | response kind mismatch: expected result=false error=true, actual result=true error=false |
| eth_estimateGas                    | estimate-failed-call.io                                                           | FAIL   | Eth exec api | Spec-only check failed | response kind mismatch: expected result=false error=true, actual result=true error=false |
| eth_getBlockByHash                 | get-block-by-empty-hash.iox                                                       | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_getBlockByHash                 | get-block-by-notfound-hash.iox                                                    | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_getBlockByNumber               | get-block-notfound.iox                                                            | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_getBlockReceipts               | get-block-receipts-empty.iox                                                      | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_getBlockReceipts               | get-block-receipts-not-found.iox                                                  | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_getBlockTransactionCountByHash | get-genesis.iox                                                                   | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_getLogs                        | filter-error-future-block-range.io                                                | FAIL   | Eth exec api | Spec-only check failed | response kind mismatch: expected result=false error=true, actual result=true error=false |
| eth_newPendingTransactionFilter    | newPendingTransactionFilter.iox                                                   | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-add-more-non-defined-BlockStateCalls-than-fit-but-now-with-fit.iox    | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-basefee-too-low-without-validation-38012-without-basefee-override.iox | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-basefee-too-low-without-validation-38012.iox                          | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-blobs.iox                                                             | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-block-timestamps-incrementing.iox                                     | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-blockhash-complex.iox                                                 | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-blockhash-simple.iox                                                  | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-blockhash-start-before-head.iox                                       | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-blocknumber-increment.iox                                             | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-check-that-balance-is-there-after-new-block.iox                       | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-check-that-nonce-increases.iox                                        | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-contract-calls-itself.iox                                             | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-empty-calls-and-overrides-ethSimulate.iox                             | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-empty-validation.iox                                                  | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-empty-with-block-num-set-current.iox                                  | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-empty-with-block-num-set-firstblock.iox                               | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-empty-with-block-num-set-minusone.iox                                 | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-empty.iox                                                             | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-eth-send-should-not-produce-logs-by-default.iox                       | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-eth-send-should-not-produce-logs-on-revert.iox                        | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-eth-send-should-produce-logs.iox                                      | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-eth-send-should-produce-more-logs-on-forward.iox                      | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-eth-send-should-produce-no-logs-on-forward-revert.iox                 | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-extcodehash-existing-contract.iox                                     | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-extcodehash-override.iox                                              | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-extcodehash-precompile.iox                                            | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-fee-recipient-receiving-funds.iox                                     | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-get-block-properties.iox                                              | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-logs.iox                                                              | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-move-ecrecover-and-call-old-and-new.iox                               | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-move-ecrecover-and-call.iox                                           | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-move-ecrecover-twice-and-call.iox                                     | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-move-two-accounts-to-same-38023.iox                                   | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-no-fields-call.iox                                                    | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-only-from-to-transaction.iox                                          | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-only-from-transaction.iox                                             | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-overflow-nonce.iox                                                    | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-override-address-twice-in-separate-BlockStateCalls.iox                | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-override-block-num.iox                                                | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-override-ecrecover.iox                                                | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-override-identity.iox                                                 | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-override-sha256.iox                                                   | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-override-storage-slots.iox                                            | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-overwrite-existing-contract.iox                                       | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-precompile-is-sending-transaction.iox                                 | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-run-gas-spending.iox                                                  | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-run-out-of-gas-in-block-38015.iox                                     | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-self-destructing-state-override.iox                                   | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-self-destructive-contract-produces-logs.iox                           | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-send-eth-and-delegate-call-to-eoa.iox                                 | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-send-eth-and-delegate-call-to-payble-contract.iox                     | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-send-eth-and-delegate-call.iox                                        | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-set-read-storage.iox                                                  | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-simple-more-params-validate.iox                                       | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-simple-send-from-contract.iox                                         | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-simple-state-diff.iox                                                 | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-simple-validation-fulltx.iox                                          | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-simple-with-validation-no-funds.iox                                   | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-simple.iox                                                            | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-transaction-too-high-nonce.iox                                        | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-transaction-too-low-nonce-38010.iox                                   | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-transfer-over-BlockStateCalls.iox                                     | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-two-blocks-with-complete-eth-sends.iox                                | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_simulateV1                     | ethSimulate-use-as-many-features-as-possible.iox                                  | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |
| eth_syncing                        | check-syncing.iox                                                                 | FAIL   | Sei          | Spec-only check failed | response kind mismatch: expected result=true error=false, actual result=false error=true |


### Skipped tests (0)

With the script setting **SEI_EVM_IO_SEED_BLOCK** and **SEI_EVM_IO_DEPLOY_TX_HASH**, no tests are skipped in the latest run. If you run `go test` without the script, some tests may skip for missing `${txHash}` or `${deployTxHash}`. When a test skips, the runner logs **[SKIP]** lines with bindings and placeholders so you can see why.

**Debug one or a few SEED tests:** Run only specific files with extra per-pair logging (request after substitution, bindings, whether `result.transactions` is present):

```bash
SEI_EVM_IO_DEBUG_FILES="debug_getRawTransaction/get-tx.iox" go test ./integration_test/evm_module/rpc_io_test/ -v -run TestEVMRPCSpec
```

Use a comma-separated list to run up to a few files, e.g. `debug_getRawTransaction/get-tx.iox,debug_traceTransaction/traceTransaction.iox`. Logs show `SEI_EVM_IO_SEED_BLOCK`, each pair's placeholders and binding values, the actual request sent, and bindings after each response.

### Summary


| Metric          | Count |
| --------------- | ----- |
| **Total tests** | 255   |
| **Passed**      | 157   |
| **Failed**      | 98    |
| **Skipped**     | 0     |
| **Pass rate**   | 61.6% |



| Metric                               | Count                                                                                                                      |
| ------------------------------------ | -------------------------------------------------------------------------------------------------------------------------- |
| **Total endpoints tested**           | 71                                                                                                                         |
| **Endpoints with at least one pass** | 71                                                                                                                         |
| **Missing / untested endpoints**     | None in this suite. Every method folder under `testdata/` is exercised; skips and failures are per-test, not per-endpoint. |


*Results are from a single local run; re-run `evm_rpc_tests.sh` to refresh.*