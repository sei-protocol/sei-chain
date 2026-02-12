package occ

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/occ_tests/messages"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/stretchr/testify/require"
)

func assertEqualState(t *testing.T, expectedCtx sdk.Context, actualCtx sdk.Context, testName string) {
	expectedStoreKeys := expectedCtx.MultiStore().StoreKeys()
	actualStoreKeys := actualCtx.MultiStore().StoreKeys()
	require.Equal(t, len(expectedStoreKeys), len(actualStoreKeys), testName)

	// store keys are mapped by reference, so Name()==Name() comparison is needed
	for _, esk := range expectedStoreKeys {
		for _, ask := range actualStoreKeys {
			if esk.Name() == ask.Name() {
				expected := expectedCtx.MultiStore().GetKVStore(esk)
				actual := actualCtx.MultiStore().GetKVStore(ask)
				utils.CompareStores(t, esk, expected, actual, testName)
			}
		}
	}
}

// assertEqualEventAttributes checks if both attribute slices have the same attributes, regardless of order.
func assertEqualEventAttributes(t *testing.T, testName string, expected, actual []types.EventAttribute) {
	require.Equal(t, len(expected), len(actual), "%s: Number of event attributes do not match", testName)

	// Convert the slice of EventAttribute to a string for comparison to avoid issues with byte slice comparison.
	attributesToString := func(attrs []types.EventAttribute) map[string]bool {
		attrStrs := make(map[string]bool)
		for _, attr := range attrs {
			attrStr := fmt.Sprintf("%s=%s/%v", attr.Key, attr.Value, attr.Index)
			attrStrs[attrStr] = true
		}
		return attrStrs
	}

	expectedAttrStrs := attributesToString(expected)
	actualAttrStrs := attributesToString(actual)

	require.Equal(t, expectedAttrStrs, actualAttrStrs, "%s: Event attributes do not match", testName)
}

// assertEqualEvents checks if both event slices have the same events, regardless of order.
func assertEqualEvents(t *testing.T, expected, actual []types.Event, testName string) {
	require.Equal(t, len(expected), len(actual), "%s: Number of events do not match", testName)

	for _, expectedEvent := range expected {
		found := false
		for i, actualEvent := range actual {
			if expectedEvent.Type == actualEvent.Type {
				assertEqualEventAttributes(t, testName, expectedEvent.Attributes, actualEvent.Attributes)
				actual = append(actual[:i], actual[i+1:]...) // Remove the found event
				found = true
				break
			}
		}
		require.True(t, found, "%s: Expected event of type '%s' not found", testName, expectedEvent.Type)
	}
}

// assertEqualExecTxResults validates the code, so that all errors don't count as a success
func assertExecTxResultCode(t *testing.T, expected, actual []*types.ExecTxResult, code uint32, testName string) {
	for _, e := range expected {
		require.Equal(t, code, e.Code, "%s: Expected code %d, got %d", testName, code, e.Code)
	}
	for _, a := range actual {
		require.Equal(t, code, a.Code, "%s: Actual code %d, got %d", testName, code, a.Code)
	}
}

// assertEqualExecTxResults checks if both slices have the same transaction results, regardless of order.
func assertEqualExecTxResults(t *testing.T, expected, actual []*types.ExecTxResult, testName string) {
	require.Equal(t, len(expected), len(actual), "%s: Number of transaction results do not match", testName)

	// Here, we assume that ExecTxResult is comparable; if not, you'll need to create a key
	// that is based on the comparable parts of the ExecTxResult.
	for _, expectedRes := range expected {
		found := false
		for _, actualRes := range actual {
			if reflect.DeepEqual(expectedRes, actualRes) {
				found = true
				break
			}
		}
		require.True(t, found, "%s: Expected ExecTxResult not found: %+v", testName, expectedRes)
	}
}

type Test struct {
	name    string
	runs    int
	shuffle bool
	before  func(tCtx *utils.TestContext)
	txs     func(tCtx *utils.TestContext) []*utils.TestMessage
}

func TestParallelTransactionsWasmInstantiate(t *testing.T) {
	runTest(t, Test{
		name: "Test wasm instantiations",
		runs: 3,
		txs: func(tCtx *utils.TestContext) []*utils.TestMessage {
			return utils.JoinMsgs(
				messages.WasmInstantiate(tCtx, 10),
			)
		},
	})
}

func TestParallelTransactionsBankTransfer(t *testing.T) {
	runTest(t, Test{
		name: "Test bank transfer",
		runs: 3,
		txs: func(tCtx *utils.TestContext) []*utils.TestMessage {
			return utils.JoinMsgs(
				messages.BankTransfer(tCtx, 2),
			)
		},
	})
}

func TestParallelTransactionsGovProposal(t *testing.T) {
	runTest(t, Test{
		name: "Test governance proposal",
		runs: 3,
		txs: func(tCtx *utils.TestContext) []*utils.TestMessage {
			return utils.JoinMsgs(
				messages.GovernanceSubmitProposal(tCtx, 10),
			)
		},
	})
}

func TestParallelTransactionsEvmTransferNonConflicting(t *testing.T) {
	runTest(t, Test{
		name: "Test evm transfers non-conflicting",
		runs: 3,
		txs: func(tCtx *utils.TestContext) []*utils.TestMessage {
			return utils.JoinMsgs(
				messages.EVMTransferNonConflicting(tCtx, 10),
			)
		},
	})
}

func TestParallelTransactionsEvmTransferConflicting(t *testing.T) {
	runTest(t, Test{
		name: "Test evm transfers conflicting",
		runs: 3,
		txs: func(tCtx *utils.TestContext) []*utils.TestMessage {
			return utils.JoinMsgs(
				messages.EVMTransferConflicting(tCtx, 10),
			)
		},
	})
}

func TestParallelTransactionsPointerCreation(t *testing.T) {
	runTest(t, Test{
		name: "Test pointer creation",
		runs: 3,
		txs: func(tCtx *utils.TestContext) []*utils.TestMessage {
			return utils.JoinMsgs(
				messages.ERC20toCWAssets(tCtx, 10),
			)
		},
	})
}

func TestParallelTransactionsCombined(t *testing.T) {
	runTest(t, Test{
		name:    "Test combinations",
		runs:    3,
		shuffle: true,
		txs: func(tCtx *utils.TestContext) []*utils.TestMessage {
			return utils.JoinMsgs(
				messages.WasmInstantiate(tCtx, 10),
				messages.BankTransfer(tCtx, 10),
				messages.GovernanceSubmitProposal(tCtx, 10),
				messages.EVMTransferConflicting(tCtx, 10),
				messages.ERC20toCWAssets(tCtx, 10),
				messages.EVMTransferNonConflicting(tCtx, 10),
			)
		},
	})
}

func runTest(t *testing.T, tt Test) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)

	// execute sequentially, then in parallel
	// the responses and state should match for both
	sCtx := utils.NewTestContext(t, accts, blockTime, 1, false)
	txs := tt.txs(sCtx)
	if tt.shuffle {
		txs = utils.Shuffle(txs)
	}

	if tt.before != nil {
		tt.before(sCtx)
	}

	sEvts, sResults, _, sErr := utils.RunWithoutOCC(sCtx, txs)
	require.NoError(t, sErr, tt.name)
	require.Len(t, sResults, len(txs))

	for i := 0; i < tt.runs; i++ {
		pCtx := utils.NewTestContext(t, accts, blockTime, config.DefaultConcurrencyWorkers, true)
		if tt.before != nil {
			tt.before(pCtx)
		}
		pEvts, pResults, _, pErr := utils.RunWithOCC(pCtx, txs)
		require.NoError(t, pErr, tt.name)
		require.Len(t, pResults, len(txs))

		assertExecTxResultCode(t, sResults, pResults, 0, tt.name)
		assertEqualEvents(t, sEvts, pEvts, tt.name)
		assertEqualExecTxResults(t, sResults, pResults, tt.name)
		assertEqualState(t, sCtx.Ctx, pCtx.Ctx, tt.name)
	}
}

// BenchmarkEVMTransactionsMixed benchmarks execution time for a mix of conflicting
// and non-conflicting EVM transactions that are shuffled together.
func BenchmarkEVMTransactionsMixed(b *testing.B) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)

	// Transaction counts
	conflictingCount := 50
	nonConflictingCount := 50
	totalTxCount := conflictingCount + nonConflictingCount

	b.Logf("Benchmark setup: %d conflicting + %d non-conflicting = %d total EVM transactions",
		conflictingCount, nonConflictingCount, totalTxCount)

	b.ResetTimer()

	var totalGasUsed int64
	var totalTimedDuration time.Duration

	// Run the benchmark
	for i := 0; i < b.N; i++ {
		// Create a fresh context and prepare transactions (outside timer)
		b.StopTimer()
		iterCtx := utils.NewTestContext(b, accts, blockTime, config.DefaultConcurrencyWorkers, true)

		// Generate transactions for this iteration using the fresh context
		conflictingTxs := messages.EVMTransferConflicting(iterCtx, conflictingCount)
		nonConflictingTxs := messages.EVMTransferNonConflicting(iterCtx, nonConflictingCount)

		mixedTxs := utils.JoinMsgs(conflictingTxs, nonConflictingTxs)
		mixedTxs = utils.Shuffle(mixedTxs)

		txs := utils.ToTxBytes(iterCtx, mixedTxs)
		b.StartTimer()

		// Only measure ProcessBlock execution time
		startTime := time.Now()
		_, txResults, _, err := utils.ProcessBlockDirect(iterCtx, txs, true)
		timedDuration := time.Since(startTime)
		totalTimedDuration += timedDuration

		if err != nil {
			b.Fatalf("ProcessBlock returned error (unexpected): %v", err)
		}
		if len(txResults) != len(txs) {
			b.Fatalf("Expected %d transaction results, got %d", len(txs), len(txResults))
		}

		// Sum gas used from all transaction results
		for _, result := range txResults {
			totalGasUsed += result.GasUsed
		}
	}

	// Report metrics
	b.ReportMetric(float64(totalTxCount), "txns/op")
	avgGasPerOp := float64(totalGasUsed) / float64(b.N)
	b.ReportMetric(avgGasPerOp, "gas/op")

	// Calculate and report gas per second
	avgTimeSeconds := totalTimedDuration.Seconds() / float64(b.N)
	gasPerSecond := avgGasPerOp / avgTimeSeconds
	b.ReportMetric(gasPerSecond, "gas/sec")
}
