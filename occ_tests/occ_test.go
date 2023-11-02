package occ_tests

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// TestParallelTransactions verifies that the store state is equivalent
// between both parallel and sequential executions
func TestParallelTransactions(t *testing.T) {
	runs := 3
	tests := []struct {
		name    string
		runs    int
		shuffle bool
		txs     func(tCtx *TestContext) []sdk.Msg
	}{
		{
			name: "Test wasm instantiations",
			runs: runs,
			txs: func(tCtx *TestContext) []sdk.Msg {
				return joinMsgs(
					wasmInstantiate(tCtx, 10),
				)
			},
		},
		{
			name: "Test bank transfer",
			runs: runs,
			txs: func(tCtx *TestContext) []sdk.Msg {
				return joinMsgs(
					bankTransfer(tCtx, 10),
				)
			},
		},
		{
			name: "Test governance proposal",
			runs: runs,
			txs: func(tCtx *TestContext) []sdk.Msg {
				return joinMsgs(
					governanceSubmitProposal(tCtx, 10),
				)
			},
		},
		{
			name:    "Test combinations",
			runs:    runs,
			shuffle: true,
			txs: func(tCtx *TestContext) []sdk.Msg {
				return joinMsgs(
					wasmInstantiate(tCtx, 10),
					bankTransfer(tCtx, 10),
					governanceSubmitProposal(tCtx, 10),
				)
			},
		},
	}

	for _, tt := range tests {
		blockTime := time.Now()
		signer := initSigner()

		// execute sequentially, then in parallel
		// the responses and state should match for both
		sCtx := initTestContext(signer, blockTime)
		txs := tt.txs(sCtx)
		if tt.shuffle {
			txs = shuffle(txs)
		}

		sEvts, sResults, _, sErr := runSequentially(sCtx, txs)
		require.NoError(t, sErr, tt.name)

		for i := 0; i < tt.runs; i++ {
			pCtx := initTestContext(signer, blockTime)
			pEvts, pResults, _, pErr := runParallel(pCtx, txs)
			require.NoError(t, pErr, tt.name)
			assertEqualEvents(t, sEvts, pEvts, tt.name)
			assertExecTxResultCode(t, sResults, pResults, 0, tt.name)
			assertEqualExecTxResults(t, sResults, pResults, tt.name)
			assertEqualState(t, sCtx.Ctx, pCtx.Ctx, tt.name)
		}
	}
}
