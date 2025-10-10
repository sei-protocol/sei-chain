package occ

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/occ_tests/messages"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	"github.com/stretchr/testify/require"
)

func TestPerfEvmTransferNonConflicting(t *testing.T) {
	runPerfTest(t, Test{
		runs:  1,
		accts: 500,
		name:  "Test evm transfers non-conflicting",
		txs: func(tCtx *utils.TestContext) []*utils.TestMessage {
			return utils.JoinMsgs(
				messages.EVMTransferNonConflicting(tCtx, 500),
			)
		},
	})
}

func runPerfTest(t *testing.T, tt Test) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(tt.accts)
	ctx := utils.NewTestContext(t, accts, blockTime, 500, true)
	if tt.before != nil {
		tt.before(ctx)
	}
	txs := tt.txs(ctx)
	if tt.shuffle {
		txs = utils.Shuffle(txs)
	}

	for range tt.runs {
		ctx = utils.NewTestContext(t, accts, blockTime, 500, true)
		if tt.before != nil {
			tt.before(ctx)
		}
		_, pResults, _, duration, pErr := utils.RunWithOCC(ctx, txs)
		require.NoError(t, pErr, tt.name)
		require.Len(t, pResults, len(txs))
		assertTxResultCode(t, pResults, 0, tt.name)
		t.Logf("duration = %v", duration)
	}

}
