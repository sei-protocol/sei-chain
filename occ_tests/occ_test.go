package occ

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/server/config"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/occ_tests/messages"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/abci/types"
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

// TestParallelTransactions verifies that the store state is equivalent
// between both parallel and sequential executions
func TestParallelTransactions(t *testing.T) {
	runs := 3
	tests := []struct {
		name    string
		runs    int
		shuffle bool
		before  func(tCtx *utils.TestContext)
		txs     func(tCtx *utils.TestContext) []*utils.TestMessage
	}{
		{
			name: "Test wasm instantiations",
			runs: runs,
			txs: func(tCtx *utils.TestContext) []*utils.TestMessage {
				return utils.JoinMsgs(
					messages.WasmInstantiate(tCtx, 10),
				)
			},
		},
		{
			name: "Test bank transfer",
			runs: runs,
			txs: func(tCtx *utils.TestContext) []*utils.TestMessage {
				return utils.JoinMsgs(
					messages.BankTransfer(tCtx, 2),
				)
			},
		},
		{
			name: "Test governance proposal",
			runs: runs,
			txs: func(tCtx *utils.TestContext) []*utils.TestMessage {
				return utils.JoinMsgs(
					messages.GovernanceSubmitProposal(tCtx, 10),
				)
			},
		},
		{
			name: "Test evm transfers non-conflicting",
			runs: runs,
			txs: func(tCtx *utils.TestContext) []*utils.TestMessage {
				return utils.JoinMsgs(
					messages.EVMTransferNonConflicting(tCtx, 10),
				)
			},
		},
		{
			name: "Test evm transfers conflicting",
			runs: runs,
			txs: func(tCtx *utils.TestContext) []*utils.TestMessage {
				return utils.JoinMsgs(
					messages.EVMTransferConflicting(tCtx, 10),
				)
			},
		},
		{
			name: "Test pointer creation",
			runs: runs,
			txs: func(tCtx *utils.TestContext) []*utils.TestMessage {
				return utils.JoinMsgs(
					messages.ERC20toCWAssets(tCtx, 10),
				)
			},
		},
		{
			name:    "Test combinations",
			runs:    runs,
			shuffle: true,
			txs: func(tCtx *utils.TestContext) []*utils.TestMessage {
				return utils.JoinMsgs(
					messages.WasmInstantiate(tCtx, 10),
					messages.BankTransfer(tCtx, 10),
					messages.GovernanceSubmitProposal(tCtx, 10),
					messages.EVMTransferConflicting(tCtx, 10),
					messages.EVMTransferNonConflicting(tCtx, 10),
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
		})
	}
}
