package evmrpc

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/stretchr/testify/require"
)

func TestNewLogBudgetCoercion(t *testing.T) {
	t.Parallel()
	f := &LogFetcher{filterConfig: &FilterConfig{maxLogBytes: 0}}
	budget := f.newLogBudget(10)
	require.NotNil(t, budget)
	require.NoError(t, budget.Reserve(&ethtypes.Log{Address: common.HexToAddress("0x1")}))
}

func TestLogBudgetByteCapFewLogs(t *testing.T) {
	t.Parallel()
	huge := make([]byte, 128<<10)
	log := &ethtypes.Log{
		Address: common.HexToAddress("0xabc"),
		Data:    huge,
	}
	maxBytes := receipt.EstimateLogHeapBytes(log) - 1
	budget := receipt.NewLogBudget(1000, maxBytes)

	err := budget.Reserve(log)
	require.ErrorIs(t, err, receipt.ErrTooManyLogBytes)
	require.True(t, budget.Tripped())
}
