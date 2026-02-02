package giga_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/giga/tests/harness"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// BlockchainTests Runner
// ============================================================================

// BlockchainComparisonConfig configures how blockchain test comparison runs
type BlockchainComparisonConfig struct {
	GigaMode           ExecutorMode // ModeGigaSequential or ModeGigaWithRegularStore
	VerifyEthereumSpec bool         // Whether to verify against Ethereum test spec expected postState
}

// TestGigaVsV2_BlockchainTests runs blockchain tests comparing Giga vs V2 execution.
// By default, uses GigaStore (ModeGigaSequential). Set USE_REGULAR_STORE=true to use
// regular KVStore instead, which isolates whether issues are in the Giga executor
// logic vs the GigaKVStore layer.
//
// Usage: BLOCKCHAIN_TEST_DIR=bcExample go test -v -run TestGigaVsV2_BlockchainTests ./giga/tests/...
// Usage with test name filter: BLOCKCHAIN_TEST_DIR=bcExample BLOCKCHAIN_TEST_NAME=shanghai go test -v ...
// Usage with regular store: BLOCKCHAIN_TEST_DIR=bcExample USE_REGULAR_STORE=true go test -v ...
// Usage to verify against Ethereum spec: BLOCKCHAIN_TEST_DIR=bcExample VERIFY_ETHEREUM_SPEC=true go test -v ...
func TestGigaVsV2_BlockchainTests(t *testing.T) {
	mode := ModeGigaSequential
	modeName := "Giga vs V2"
	if os.Getenv("USE_REGULAR_STORE") == "true" {
		mode = ModeGigaWithRegularStore
		modeName = "Giga with Regular Store"
	}
	runBlockchainTestSuite(t, BlockchainComparisonConfig{
		GigaMode:           mode,
		VerifyEthereumSpec: os.Getenv("VERIFY_ETHEREUM_SPEC") == "true",
	}, modeName)
}

// runBlockchainTestSuite runs the blockchain test suite with the given configuration.
func runBlockchainTestSuite(t *testing.T, config BlockchainComparisonConfig, summaryName string) {
	blockchainTestsPath, err := harness.GetBlockchainTestsPath()
	require.NoError(t, err, "failed to get path to blockchain tests")

	// Load skip list
	skipList, err := harness.LoadSkipList()
	require.NoError(t, err, "failed to load skip list")

	// Allow filtering to specific directory via BLOCKCHAIN_TEST_DIR env var
	specificDir := os.Getenv("BLOCKCHAIN_TEST_DIR")
	if specificDir == "" {
		t.Skip("BLOCKCHAIN_TEST_DIR not set - skipping blockchain tests")
	}

	// Determine test type (ValidBlocks or InvalidBlocks)
	testType := os.Getenv("BLOCKCHAIN_TEST_TYPE")
	if testType == "" {
		testType = "ValidBlocks"
	}

	// Allow filtering to specific test name via BLOCKCHAIN_TEST_NAME env var
	specificTestName := os.Getenv("BLOCKCHAIN_TEST_NAME")

	// Allow filtering to specific test index via BLOCKCHAIN_TEST_INDEX env var
	specificIndexStr := os.Getenv("BLOCKCHAIN_TEST_INDEX")
	specificIndex := -1
	if specificIndexStr != "" {
		var parseErr error
		specificIndex, parseErr = strconv.Atoi(specificIndexStr)
		if parseErr != nil {
			t.Fatalf("Invalid BLOCKCHAIN_TEST_INDEX: %s", specificIndexStr)
		}
	}

	// Allow including non-Cancun network tests
	includeAllNetworks := os.Getenv("INCLUDE_ALL_NETWORKS") == "true"

	// Allow bypassing skip list for analysis purposes
	ignoreSkipList := os.Getenv("IGNORE_SKIP_LIST") == "true"

	// Check if entire category is skipped
	categoryKey := fmt.Sprintf("blockchain/%s/%s", testType, specificDir)
	if !ignoreSkipList && skipList.IsCategorySkipped(categoryKey) {
		t.Skipf("Skipping category %s (in skip list)", categoryKey)
	}

	dirPath := blockchainTestsPath + "/" + testType + "/" + specificDir

	tests, err := harness.LoadBlockchainTestsFromDir(dirPath)
	require.NoError(t, err, "failed to load blockchain tests from %s", dirPath)

	if len(tests) == 0 {
		t.Skipf("No blockchain tests found in %s", dirPath)
	}

	t.Logf("Found %d blockchain test entries in %s", len(tests), specificDir)
	if specificTestName != "" {
		t.Logf("Filtering to tests matching: %s", specificTestName)
	}

	// Track results for summary
	summary := NewTestSummary()

	// Print summary at the end
	t.Cleanup(func() {
		t.Logf("\n=== %s Blockchain Test Summary ===", summaryName)
		summary.PrintSummary(t)
	})

	testIndex := 0
	for testName, test := range tests {
		// Filter by test name if specified
		if specificTestName != "" && !strings.Contains(testName, specificTestName) {
			continue
		}

		// Filter by index if specified
		if specificIndex >= 0 && testIndex != specificIndex {
			testIndex++
			continue
		}
		testIndex++

		// Filter by network (default to Cancun only)
		if !includeAllNetworks && test.Network != "Cancun" {
			continue
		}

		subtestName := testName

		// Check skip list (unless bypassed for analysis)
		if !ignoreSkipList {
			skipKey := fmt.Sprintf("blockchain/%s/%s/%s", testType, specificDir, testName)
			if shouldSkip, reason := skipList.ShouldSkip(categoryKey, skipKey); shouldSkip {
				t.Run(subtestName, func(t *testing.T) {
					summary.RecordSkip(categoryKey, subtestName, reason)
					t.Skipf("Skipped: %s", reason)
				})
				continue
			}
		}

		// Capture variables for closure
		category := categoryKey
		testNameCopy := subtestName
		testCopy := test

		t.Run(subtestName, func(t *testing.T) {
			result := runBlockchainTestComparison(t, testCopy, config)
			if result.Passed {
				summary.RecordPass(category, testNameCopy)
			} else {
				summary.RecordFailure(category, testNameCopy, result.FailureType, result.Message)
				t.Errorf("Blockchain test comparison failed: %s - %s", result.FailureType, result.Message)
				for _, detail := range result.Details {
					t.Logf("  %s", detail)
				}
			}
		})
	}
}

// runBlockchainTestComparison runs a blockchain test and returns detailed results.
func runBlockchainTestComparison(t *testing.T, test *harness.BlockchainTestJSON, config BlockchainComparisonConfig) TestResult {
	blockTime := time.Now()

	// --- Setup V2 context ---
	v2Ctx := NewStateTestContext(t, blockTime, 1, ModeV2Sequential)
	v2Ctx.SetupPreState(t, test.Pre)

	// --- Setup Giga context ---
	gigaCtx := NewStateTestContext(t, blockTime, 1, config.GigaMode)
	gigaCtx.SetupPreState(t, test.Pre)

	// Setup genesis block environment
	genesisEnv := harness.BlockEnvFromHeader(test.GenesisBlockHeader)
	v2Ctx.SetupEnv(genesisEnv)
	gigaCtx.SetupEnv(genesisEnv)

	// Process each block in sequence
	for blockIdx, block := range test.Blocks {
		// Build transactions from JSON fields (preferred method for BlockchainTests)
		// This avoids the need to re-sign transactions with Sei's chain ID
		var signedTxs []*ethtypes.Transaction
		var senders []common.Address

		if len(block.Transactions) > 0 {
			signedTxs = make([]*ethtypes.Transaction, len(block.Transactions))
			senders = make([]common.Address, len(block.Transactions))
			for i, tx := range block.Transactions {
				signedTx, sender, err := harness.BuildBlockchainTransactionFromJSON(tx)
				if err != nil {
					// If we can't build from JSON, try to decode from RLP as fallback
					if block.RLP != "" {
						decodedTxs, decodedSenders, rlpErr := harness.DecodeBlockTransactions(block.RLP)
						if rlpErr != nil {
							return TestResult{
								Passed:      false,
								FailureType: FailureTypeUnknown,
								Message:     fmt.Sprintf("block %d tx %d: failed to build transaction: %v (RLP fallback also failed: %v)", blockIdx, i, err, rlpErr),
							}
						}
						// Re-sign with Sei chain ID
						resignedTxs, resignErr := harness.ResignBlockTransactionsForSei(decodedTxs, decodedSenders)
						if resignErr != nil {
							return TestResult{
								Passed:      false,
								FailureType: FailureTypeUnknown,
								Message:     fmt.Sprintf("block %d: failed to re-sign transactions: %v", blockIdx, resignErr),
							}
						}
						signedTxs = resignedTxs
						senders = decodedSenders
						break
					}
					return TestResult{
						Passed:      false,
						FailureType: FailureTypeUnknown,
						Message:     fmt.Sprintf("block %d tx %d: failed to build transaction: %v", blockIdx, i, err),
					}
				}
				signedTxs[i] = signedTx
				senders[i] = sender
			}
		}

		// Setup block environment
		blockEnv := harness.BlockEnvFromHeader(block.BlockHeader)
		v2Ctx.SetupEnv(blockEnv)
		gigaCtx.SetupEnv(blockEnv)

		// Encode transactions for the app
		txBytes := make([][]byte, len(signedTxs))
		for i, signedTx := range signedTxs {
			// Setup sender address mapping for each transaction
			v2Ctx.SetupSender(senders[i])
			gigaCtx.SetupSender(senders[i])

			encoded, err := harness.EncodeTxForApp(signedTx)
			if err != nil {
				return TestResult{
					Passed:      false,
					FailureType: FailureTypeUnknown,
					Message:     fmt.Sprintf("block %d tx %d: failed to encode transaction: %v", blockIdx, i, err),
				}
			}
			txBytes[i] = encoded
		}

		// Execute the block on V2
		_, v2Results, v2Err := RunStateTestBlock(v2Ctx, txBytes)

		// Execute the block on Giga
		_, gigaResults, gigaErr := RunStateTestBlock(gigaCtx, txBytes)

		// Log results for debugging
		if len(v2Results) > 0 && len(gigaResults) > 0 {
			for i := range v2Results {
				if i < len(gigaResults) {
					t.Logf("Block %d tx[%d] V2: code=%d gas=%d | Giga: code=%d gas=%d",
						blockIdx, i, v2Results[i].Code, v2Results[i].GasUsed, gigaResults[i].Code, gigaResults[i].GasUsed)
				}
			}
		}

		// --- Compare execution errors ---
		if v2Err != nil && gigaErr != nil {
			// Both failed - check if same type of failure
			t.Logf("Block %d: Both executors failed: v2=%v, giga=%v", blockIdx, v2Err, gigaErr)
			continue // Move to next block
		}
		if v2Err != nil {
			return TestResult{
				Passed:      false,
				FailureType: FailureTypeV2Error,
				Message:     fmt.Sprintf("block %d: V2 execution failed but Giga succeeded: %v", blockIdx, v2Err),
			}
		}
		if gigaErr != nil {
			return TestResult{
				Passed:      false,
				FailureType: FailureTypeGigaError,
				Message:     fmt.Sprintf("block %d: Giga execution failed but V2 succeeded: %v", blockIdx, gigaErr),
			}
		}

		// --- Compare results ---
		if len(v2Results) != len(gigaResults) {
			return TestResult{
				Passed:      false,
				FailureType: FailureTypeUnknown,
				Message:     fmt.Sprintf("block %d: result count mismatch: V2=%d, Giga=%d", blockIdx, len(v2Results), len(gigaResults)),
			}
		}

		for i := range v2Results {
			v2Failed := v2Results[i].Code != 0
			gigaFailed := gigaResults[i].Code != 0

			// One succeeded, one failed = definite mismatch
			if v2Failed != gigaFailed {
				details := []string{
					fmt.Sprintf("V2: code=%d log=%q", v2Results[i].Code, v2Results[i].Log),
					fmt.Sprintf("Giga: code=%d log=%q", gigaResults[i].Code, gigaResults[i].Log),
				}
				return TestResult{
					Passed:      false,
					FailureType: FailureTypeResultCode,
					Message:     fmt.Sprintf("block %d tx[%d] V2 %s (code=%d), Giga %s (code=%d)", blockIdx, i, statusStr(v2Failed), v2Results[i].Code, statusStr(gigaFailed), gigaResults[i].Code),
					Details:     details,
				}
			}

			// Both failed - check if codes or gas differ
			// KNOWN ISSUE: V2 and Giga report different error codes and gas for failed transactions.
			// V2 uses Cosmos SDK error codes (e.g., 32 for sequence mismatch) and reports Cosmos gas.
			// Giga uses EVM-style codes (e.g., 1 for generic failure) and reports 0 gas.
			// These tests should be skipped until this behavior is unified.
			if v2Failed && gigaFailed {
				if v2Results[i].Code != gigaResults[i].Code || v2Results[i].GasUsed != gigaResults[i].GasUsed {
					details := []string{
						fmt.Sprintf("V2: code=%d gas=%d log=%q", v2Results[i].Code, v2Results[i].GasUsed, v2Results[i].Log),
						fmt.Sprintf("Giga: code=%d gas=%d log=%q", gigaResults[i].Code, gigaResults[i].GasUsed, gigaResults[i].Log),
						"KNOWN ISSUE: V2 and Giga differ in error code/gas reporting for failed transactions",
					}
					return TestResult{
						Passed:      false,
						FailureType: FailureTypeFailedTxBehavior,
						Message:     fmt.Sprintf("block %d tx[%d] failed_tx_mismatch: V2(code=%d,gas=%d) vs Giga(code=%d,gas=%d)", blockIdx, i, v2Results[i].Code, v2Results[i].GasUsed, gigaResults[i].Code, gigaResults[i].GasUsed),
						Details:     details,
					}
				}
			}

			// Both succeeded - compare gas used
			if !v2Failed && !gigaFailed && v2Results[i].GasUsed != gigaResults[i].GasUsed {
				return TestResult{
					Passed:      false,
					FailureType: FailureTypeGasMismatch,
					Message:     fmt.Sprintf("block %d tx[%d] gas mismatch: V2=%d, Giga=%d", blockIdx, i, v2Results[i].GasUsed, gigaResults[i].GasUsed),
					Details: []string{
						fmt.Sprintf("V2 gas used: %d", v2Results[i].GasUsed),
						fmt.Sprintf("Giga gas used: %d", gigaResults[i].GasUsed),
					},
				}
			}
		}
	}

	// --- Compare V2 vs Giga post-state ---
	stateDiffs := comparePostStates(t, v2Ctx, gigaCtx, test.Pre)
	if len(stateDiffs) > 0 {
		t.Logf("V2 vs Giga state differences:")
		for _, diff := range stateDiffs {
			t.Logf("  %s", diff.Summary)
		}
		return TestResult{
			Passed:      false,
			FailureType: stateDiffs[0].Type,
			Message:     stateDiffs[0].Summary,
			Details:     formatStateDiffs(stateDiffs),
		}
	}

	// --- Verify against Ethereum spec (if configured) ---
	if config.VerifyEthereumSpec && len(test.PostState) > 0 {
		v2Diffs := verifyPostStateWithResult(t, v2Ctx.Ctx, v2Ctx.EvmKeeper(), v2Ctx.BankKeeper(), test.PostState, "V2")
		gigaDiffs := verifyPostStateWithResult(t, gigaCtx.Ctx, gigaCtx.EvmKeeper(), gigaCtx.BankKeeper(), test.PostState, "Giga")

		// Log any fixture verification differences
		if len(v2Diffs) > 0 {
			t.Logf("V2 vs fixture differences:")
			for _, diff := range v2Diffs {
				t.Logf("  %s", diff.Summary)
			}
		}
		if len(gigaDiffs) > 0 {
			t.Logf("Giga vs fixture differences:")
			for _, diff := range gigaDiffs {
				t.Logf("  %s", diff.Summary)
			}
			// Return the first Giga diff as the failure
			return TestResult{
				Passed:      false,
				FailureType: gigaDiffs[0].Type,
				Message:     gigaDiffs[0].Summary,
				Details:     formatStateDiffs(gigaDiffs),
			}
		}
	}

	return TestResult{Passed: true}
}

// statusStr returns "failed" or "succeeded" based on failure status
func statusStr(failed bool) string {
	if failed {
		return "failed"
	}
	return "succeeded"
}
