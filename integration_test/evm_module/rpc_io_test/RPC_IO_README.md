# EVM RPC .io / .iox tests

Integration tests for Sei EVM RPC compatibility with Ethereum JSON-RPC. The suite runs **159** `.io`/`.iox` files from `testdata/` against a live RPC endpoint (**157** after main's explicit-unsupported fixture refresh, plus **two** `sei_legacy_deprecation/*.iox` for gated `sei_*` and deprecation signaling).

### `.io` vs `.iox`

- **`.io`** - vanilla JSON-RPC fixtures (`>>` / `<<`): no Sei-specific harness directives such as `@ expect_body_contains`.
- **`.iox`** - same line format, plus Sei extensions: `@ bind` / `<< @ ref_pair`, `@ expect_body_contains`, `@ expect_response_header`, `not-supported.iox` (documented `-32000` errors), and other non-vanilla tags the parser accepts.

## How to run

1. Start the local cluster: `make docker-cluster-start` (EVM RPC on port 8545).
2. Run the script from repo root:
  ```bash
   ./integration_test/evm_module/scripts/evm_rpc_tests.sh
  ```

When the target is localhost, the script sends one EVM tx and deploys one contract inside the node container before `go test`, so data-dependent `.iox` tests have block/tx/contract. Default RPC URL: `http://127.0.0.1:8545` (override with `SEI_EVM_RPC_URL`).

**Legacy `sei_*` / `sei2_*` gating:** The docker localnet `app.toml` enables every gated method except **`sei_sign`** (low partner risk: not reported by Binance/Dune/Alchemy; unused by other fixtures). Deprecation is asserted in `testdata/sei_legacy_deprecation/*.iox`: **gate errors** use `error.data` `legacy_sei_deprecated` and messages mentioning disabled + deprecated; **successful** allowlisted calls use `@ expect_response_header Sei-Legacy-RPC-Deprecation` (JSON body unchanged). Directives:
- `@ expect_body_contains substring` - response body must contain the substring.
- `@ expect_response_header Header-Name` - response must include that HTTP header (case-insensitive lookup).

Production `seid init` defaults remain the three-method allowlist (`sei_getSeiAddress`, `sei_getEVMAddress`, `sei_getCosmosTx`).

### Comparing legacy vs giga (RPC parity)

To check that **giga** behaves like **legacy** at the spec level (same methods return result vs error):

**How you know which executor you're hitting:** The test suite does **not** detect or label whether the node uses giga or legacy. It only sends JSON-RPC to the URL in `SEI_EVM_RPC_URL`. You determine which executor is under test by how you started the node or which URL you pass. For parity, run once against a node you know is legacy and once against a node you know is giga, then compare.

**Running a local cluster with Giga enabled:** Pass env vars into `make docker-cluster-start` so the nodes start with the giga executor:

```bash
# All 4 nodes use Giga (and OCC). Foreground:
GIGA_EXECUTOR=true GIGA_OCC=true make docker-cluster-start

# Same, but run cluster in background so you can run the RPC test script:
GIGA_EXECUTOR=true GIGA_OCC=true DOCKER_DETACH=true make docker-cluster-start
# Wait for build/generated/launch.complete (4 lines), then:
./integration_test/evm_module/scripts/evm_rpc_tests.sh
```

Without `GIGA_EXECUTOR` and `GIGA_OCC`, the cluster uses the legacy (V2) executor. The Makefile passes these through to `docker compose`; the node image uses them in `docker/localnode/scripts/step4_config_override.sh`.

1. Run the suite against the **legacy** endpoint and record the final report:
   ```bash
   SEI_EVM_RPC_URL=<legacy_url> ./integration_test/evm_module/scripts/evm_rpc_tests.sh
   ```
   At the end you'll see a block like:
   ```
   ========== Sei EVM RPC .io/.iox test report ==========
     Total:  ...
     Passed: ...
     Failed: ...
     Skipped: ...
     Pass rate: ...%
   =======================================================
   ```
2. Run the same suite against the **giga** endpoint:
   ```bash
   SEI_EVM_RPC_URL=<giga_url> ./integration_test/evm_module/scripts/evm_rpc_tests.sh
   ```
3. Compare **Total**, **Passed**, **Failed**, and **Skipped**. Same numbers => spec parity for that run. Any difference indicates a method that returns result on one node and error on the other (or vice versa).

For a fair comparison, both endpoints should serve the **same chain** (same genesis and blocks). If using the script's seed (deploy tx), run the script once to create the seed on one node; for the second run you can point at the other node only if it has the same chain and the same block containing that deploy (e.g. two nodes in the same network).

## Test mix


| Kind      | Count | Description                                                                                                                              |
| --------- | ----- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| **.io**   | ~50   | Request/response fixtures; curated from [ethereum/execution-apis](https://github.com/ethereum/execution-apis) plus Sei-added.            |
| **.iox**  | ~109  | Sei-generated; use `@ bind` and optional `@ ref_pair N` so data comes from a first request; includes `not-supported.iox`, `sei_legacy_deprecation/*.iox`. |
| **Total** | 159   | All under `testdata/`; runner executes every .io and .iox file.                                                                          |


Fixtures live in `testdata/`; see `testdata/README.md` (do not overwrite with a raw copy from execution-apis).

### Removed tests

The following fixtures were **removed** (no longer in the suite) because they depended on execution-apis testnet state (fixed contract addresses that do not exist on Sei). Revert/estimateGas behavior is covered by self-contained Sei fixtures instead.

| Removed fixture | Reason | Replacement |
| ----------------- | ------ | ----------- |
| `eth_call/call-revert-abi-error.io` | Fixed address `0x0ee3ab...` (reverting contract on execution-apis only) | `eth_call/call-revert-abi-error-sei.iox` (uses `__REVERTER__`, script-deployed) |
| `eth_call/call-revert-abi-panic.io` | Same fixed address, panic case | `eth_call/call-revert-abi-panic-sei.iox` (uses `__REVERTER__` with input `0x02` for panic) |
| `eth_estimateGas/estimate-call-abi-error.io` | Same fixed address, expects revert error | `eth_estimateGas/estimate-call-abi-error-sei.iox` (uses `__REVERTER__`) |
| `eth_estimateGas/estimate-failed-call.io` | Fixed address `0x17e7ee...`, expects revert error | Revert (Error) and panic covered by `estimate-call-abi-error-sei.iox` and `estimate-call-abi-panic-sei.iox` (same `__REVERTER__`, input `0x01` / `0x02`) |

The total count reflects the current `.io`/`.iox` set under `testdata/` (159 files: main baseline plus sei deprecation `.iox`).

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
5. The script also deploys a **reverter** contract; it sets `SEI_EVM_IO_REVERTER_ADDRESS`. In fixtures, `__REVERTER__` is replaced by that address. The reverter responds to calldata: empty or `0x01` -> `Error("user error")`; `0x02` -> panic (assert false). Used by `eth_call/call-revert-abi-error-sei.iox`, `eth_call/call-revert-abi-panic-sei.iox`, `eth_estimateGas/estimate-call-abi-error-sei.iox`, and `eth_estimateGas/estimate-call-abi-panic-sei.iox`. If a fixture uses `__REVERTER__` and the env is not set, the test is skipped.

So "seed" = a known-good block (and deploy tx) that the script creates and the runner uses so **SEED** and deploy/tx bindings resolve.

---

## Test results (reference runs)

*Source:* **Eth exec api** = from [ethereum/execution-apis](https://github.com/ethereum/execution-apis) (`.io`); **Sei** = Sei-generated (`.iox` or Sei-added `.io`).

**Latest recorded `TestEVMRPCSpec` (docker localnet, 159 files):** **147** passed, **12** failed, **92.5%** pass rate (**eth_call fix** column below). **All `sei_*` fixtures pass** (every `testdata/**/sei_*` file, including `sei_legacy_deprecation/*.iox` for HTTP gate + `Sei-Legacy-RPC-Deprecation` header). The **12** failures are **only** `eth_*` (createAccessList insufficient funds on `from=0x0`, estimateGas parse paths, proofs, log range, block-not-found shape, tx-by-index — see *Failed tests* below). There are **no** `sei2_*` fixtures in `testdata/` yet.

**Column guide (Summary table below):** **First run** = historical full suite before trimming. **Post-trim baseline** = early **164**-fixture snapshot. **unsupported-fix** = **157** fixtures: current `testdata/` after `eth_simulateV1` removal and **`not-supported.iox`** explicit errors (see [docs/evm_jsonrpc_unsupported.md](../../../docs/evm_jsonrpc_unsupported.md)). **sei_* fix** = **159** files: **157** + two `sei_legacy_deprecation/*.iox`, docker `enabled_legacy_sei_apis` expanded (all gated `sei_*` / `sei2_*` except `sei_sign`); filter-log lifecycle `.iox` use `latest`->`latest` so they respect `max_blocks_for_log`. **eth_call fix** = same **159** files after fixture updates for **EIP-1559-shaped** `eth_call` and `eth_createAccessList`: `call-callenv-options-eip1559.iox` uses zero `maxFeePerGas`/`maxPriorityFeePerGas` with `from=0x0` (Geth `CallDefaults`/`NoBaseFee` path); `create-al-contract-eip1559.iox` uses deploy receipt `from` + non-zero caps (`setFeeDefaults` + `BuyGas`).

### Passed tests (147 of 159)


| Endpoint                               | Test                                                           | Source       |
| -------------------------------------- | -------------------------------------------------------------- | ------------ |
| cross_check                            | get-block-by-number-then-by-hash.iox                           | Sei          |
| debug_getRawBlock                      | not-supported.iox                                              | Sei          |
| debug_getRawHeader                     | not-supported.iox                                              | Sei          |
| debug_getRawReceipts                   | not-supported.iox                                              | Sei          |
| debug_getRawTransaction                | not-supported.iox                                              | Sei          |
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
| eth_blobBaseFee                        | blobs-not-supported-error.iox                                  | Sei          |
| eth_blockNumber                        | simple-test.io                                                 | Eth exec api |
| eth_call                               | call-callenv.io                                                | Eth exec api |
| eth_call                               | call-callenv-options-eip1559.iox                               | Sei          |
| eth_call                               | call-contract-from-deploy.iox                                  | Sei          |
| eth_call                               | call-contract.io                                               | Eth exec api |
| eth_call                               | call-eip7702-delegation.io                                     | Eth exec api |
| eth_call                               | call-revert-abi-error-sei.iox                                  | Sei          |
| eth_call                               | call-revert-abi-panic-sei.iox                                  | Sei          |
| eth_chainId                            | get-chain-id.io                                                | Eth exec api |
| eth_coinbase                           | coinbase.io                                                    | Sei          |
| eth_createAccessList                   | create-al-contract-eip1559.iox                                 | Sei          |
| eth_createAccessList                   | create-al-value-transfer.iox                                   | Sei          |
| eth_estimateGas                        | estimate-call-abi-error-sei.iox                                | Sei          |
| eth_estimateGas                        | estimate-call-abi-panic-sei.iox                                | Sei          |
| eth_estimateGas                        | estimate-gas-from-deploy.iox                                   | Sei          |
| eth_estimateGas                        | estimate-simple-transfer.io                                    | Eth exec api |
| eth_estimateGas                        | estimate-successful-call.io                                    | Eth exec api |
| eth_feeHistory                         | fee-history.io                                                 | Eth exec api |
| eth_gasPrice                           | gasPrice.io                                                    | Sei          |
| eth_getBalance                         | get-balance-blockhash.iox                                      | Sei          |
| eth_getBalance                         | get-balance-unknown-account.io                                 | Eth exec api |
| eth_getBalance                         | get-balance.io                                                 | Eth exec api |
| eth_getBlockByHash                     | get-block-by-empty-hash.iox                                    | Sei          |
| eth_getBlockByHash                     | get-block-by-hash.iox                                          | Sei          |
| eth_getBlockByHash                     | get-block-by-notfound-hash.iox                                 | Sei          |
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
| eth_getBlockReceipts                   | get-block-receipts-empty.iox                                   | Sei          |
| eth_getBlockReceipts                   | get-block-receipts-not-found.iox                               | Sei          |
| eth_getBlockReceipts                   | get-receipts-by-latest-block.iox                               | Sei          |
| eth_getBlockTransactionCountByHash     | get-block-n.iox                                                | Sei          |
| eth_getBlockTransactionCountByHash     | get-genesis.iox                                                | Sei          |
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
| eth_newPendingTransactionFilter        | not-supported.iox                                              | Sei          |
| eth_syncing                            | not-supported.iox                                              | Sei          |
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
| sei_legacy_deprecation                 | deprecation-success.iox                                        | Sei          |
| sei_legacy_deprecation                 | sei_sign-disabled.iox                                          | Sei          |
| sei_newBlockFilter                     | newBlockFilter.io                                              | Sei          |
| sei_newFilter                          | newFilter.io                                                   | Sei          |
| sei_traceBlockByHashExcludeTraceFail   | traceBlockByHashExcludeTraceFail.iox                           | Sei          |
| sei_traceBlockByNumberExcludeTraceFail | traceBlockByNumberExcludeTraceFail.iox                         | Sei          |
| sei_uninstallFilter                    | uninstallFilter.io                                             | Sei          |
| txpool_content                         | content.io                                                     | Sei          |
| web3_clientVersion                     | clientVersion.io                                               | Sei          |


### Failed tests (12 on latest 159-file reference run after **eth_call fix**; all `sei_*` pass)

Methods that Sei documents as unsupported use dedicated **`not-supported.iox`** fixtures (and `eth_blobBaseFee/blobs-not-supported-error.iox`). They return JSON-RPC **error** `-32000` with a fixed message (not `-32601`). See [docs/evm_jsonrpc_unsupported.md](../../../docs/evm_jsonrpc_unsupported.md).

*Remaining failures* are **only** `eth_*`: gas / base fee, proofs (IAVL), log range vs spec, block-not-found shape, tx-by-index. **No `sei_*` rows.** On **docker localnet** with expanded `enabled_legacy_sei_apis`, some historically flaky `eth_*` cases pass when the node returns `null` for missing blocks (varies by build/config).

| Endpoint                           | Test                                                                              | Status | Source       | Reason                 | Error message                                                                            |
| ---------------------------------- | --------------------------------------------------------------------------------- | ------ | ------------ | ---------------------- | ---------------------------------------------------------------------------------------- |
| eth_createAccessList               | create-al-abi-revert.iox                                                          | FAIL   | Sei          | Insufficient funds     | error code=-32000 message="insufficient funds for gas * price + value" (`from` defaults to `0x0`) |
| eth_createAccessList               | create-al-contract.iox                                                             | FAIL   | Sei          | Insufficient funds     | error code=-32000 message="insufficient funds for gas * price + value" |
| eth_estimateGas                    | estimate-with-eip4844.iox                                                         | FAIL   | Sei          | Parse error            | error code=-32700 message="parse error" |
| eth_estimateGas                    | estimate-with-eip7702.iox                                                         | FAIL   | Sei          | Parse error            | error code=-32700 message="parse error" |
| eth_estimateGasAfterCalls          | estimateGasAfterCalls.iox                                                         | FAIL   | Sei          | Insufficient funds     | error code=-32000 message="insufficient funds for gas * price + value" |
| eth_getBlockByNumber               | get-block-notfound.iox                                                            | FAIL   | Sei          | Block not available    | error code=-32000 message="requested height 1000 is not yet available; safe latest is ..." (height varies) |
| eth_getLogs                        | filter-error-future-block-range.io                                                | FAIL   | Eth exec api | Expected error, got result | response kind mismatch: expected result=false error=true, actual result=true error=false |
| eth_getProof                       | get-account-proof-blockhash.iox                                                   | FAIL   | Sei          | Store not found        | error code=-32000 message="cannot find EVM IAVL store" |
| eth_getProof                       | get-account-proof-latest.iox                                                      | FAIL   | Sei          | Store not found        | error code=-32000 message="cannot find EVM IAVL store" |
| eth_getProof                       | get-account-proof-with-storage.iox                                                | FAIL   | Sei          | Store not found        | error code=-32000 message="cannot find EVM IAVL store" |
| eth_getTransactionByBlockHashAndIndex | get-block-n.iox                                                                | FAIL   | Sei          | Index out of range     | error code=-32000 message="transaction index out of range" |
| eth_getTransactionByBlockNumberAndIndex | get-block-n.iox                                                            | FAIL   | Sei          | Index out of range     | error code=-32000 message="transaction index out of range" |


### Skipped tests (0)

With the script setting **SEI_EVM_IO_SEED_BLOCK** and **SEI_EVM_IO_DEPLOY_TX_HASH**, no tests are skipped in the reference runs above. If you run `go test` without the script, some tests may skip for missing `${txHash}` or `${deployTxHash}`. When a test skips, the runner logs **[SKIP]** lines with bindings and placeholders so you can see why.

**Debug one or a few SEED tests:** Run only specific files with extra per-pair logging (request after substitution, bindings, whether `result.transactions` is present):

```bash
SEI_EVM_IO_RUN_INTEGRATION=1 SEI_EVM_IO_DEBUG_FILES="debug_getRawTransaction/not-supported.iox" go test ./integration_test/evm_module/rpc_io_test/ -v -run TestEVMRPCSpec
```

Use a comma-separated list to run up to a few files, e.g. `debug_getRawTransaction/not-supported.iox,debug_traceTransaction/traceTransaction.iox`. Logs show `SEI_EVM_IO_SEED_BLOCK`, each pair's placeholders and binding values, the actual request sent, and bindings after each response.

### Summary (recorded runs)


| Metric | First run | Post-trim baseline | unsupported-fix | sei_* fix | eth_call fix |
| ------ | --------- | ------------------- | ---------------- | --------- | ------------ |
| **Total tests** | 255 | 164 | 157 | 159 | 159 |
| **Passed** | 157 | 135 | 142 | 145 | 147 |
| **Failed** | 98 | 29 | 15 | 14 | 12 |
| **Skipped** | 0 | 0 | 0 | 0 | 0 |
| **Pass rate** | 61.6% | 82.3% | ~90.4% | 91.2% | 92.5% |

(1) **First run** / **Post-trim** = historical snapshots. (2) **unsupported-fix** = **157** fixtures with `not-supported.iox` and related explicit unsupported RPC behavior (see [docs/evm_jsonrpc_unsupported.md](../../../docs/evm_jsonrpc_unsupported.md)); representative run ~142 pass / ~15 fail. (3) **sei_* fix (159)** = **157** + `sei_legacy_deprecation/*.iox`; docker `enabled_legacy_sei_apis` per `docker/localnode/config/app.toml` (gated `sei_*` / `sei2_*`). Reference `TestEVMRPCSpecSummary`: **145 / 14 / 91.2%**. (4) **eth_call fix (159)** = same docker localnet + script after EIP-1559 fixture updates (`call-callenv-options-eip1559.iox`, `create-al-contract-eip1559.iox`); reference **147 / 12 / 92.5%**. Associate setup may log `result: null` or an error for `sei_associate` in the script; deploy still proceeds when the tx succeeds.

**Legacy `sei_*`:** All `sei_*` fixtures pass with expanded allowlist (including `sei_legacy_deprecation/*.iox` and filter lifecycle `.iox`). Remaining fails are **`eth_*` only**, not gated `sei_*` denial. After **eth_call fix**, the only remaining **`eth_createAccessList`** failures are **`create-al-abi-revert.iox`** and **`create-al-contract.iox`** (both use implicit `from=0x0` and hit insufficient funds on `BuyGas`).



| Metric                               | Count                                                                                                                      |
| ------------------------------------ | -------------------------------------------------------------------------------------------------------------------------- |
| **Total endpoints tested**           | 70                                                                                                                          |
| **Endpoints with at least one pass** | ~60                                                                                                                         |
| **Missing / untested endpoints**    | None in this suite. Every method folder under `testdata/` is exercised; skips and failures are per-test, not per-endpoint. |

**eth_simulateV1**: that folder (1 endpoint, 64 fixtures) is no longer under `testdata/`, it was removed, so the current suite has **70** endpoint folders.


*Re-run `./integration_test/evm_module/scripts/evm_rpc_tests.sh` to refresh counts; **sei_* fix** / **eth_call fix** columns match docker localnet with expanded `[evm].enabled_legacy_sei_apis` (see `docker/localnode/config/app.toml`).*
