package evmrpc

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/stretchr/testify/require"
)

// TestGetLogsByFiltersWithoutReceiptStore pins that a node with rs-enable = false
// fails log queries instead of answering [] for ranges that contained logs.
func TestGetLogsByFiltersWithoutReceiptStore(t *testing.T) {
	t.Parallel()
	f := &LogFetcher{k: &keeper.Keeper{}}

	logs, _, err := f.GetLogsByFilters(context.Background(), filters.FilterCriteria{}, 0)
	require.ErrorIs(t, err, receipt.ErrNotConfigured)
	require.Nil(t, logs)

	// BlockHash queries skip the range path and would otherwise fall through to
	// collectLogs, which used to log-and-continue into an empty result.
	blockHash := common.HexToHash("0xabc")
	logs, _, err = f.GetLogsByFilters(context.Background(), filters.FilterCriteria{BlockHash: &blockHash}, 0)
	require.ErrorIs(t, err, receipt.ErrNotConfigured)
	require.Nil(t, logs)
}

func TestTryFilterLogsRangeWithoutReceiptStore(t *testing.T) {
	t.Parallel()
	f := &LogFetcher{k: &keeper.Keeper{}}

	logs, err := f.tryFilterLogsRange(context.Background(), 1, 2, filters.FilterCriteria{}, 10)
	require.ErrorIs(t, err, receipt.ErrNotConfigured)
	require.Nil(t, logs)
}

func TestFilterTransactionsWithoutReceiptStore(t *testing.T) {
	t.Parallel()
	_, err := filterTransactions(&keeper.Keeper{}, nil, nil, nil, false, nil, nil)
	require.ErrorIs(t, err, receipt.ErrNotConfigured)
}

func TestFeeHistoryWithoutReceiptStore(t *testing.T) {
	t.Parallel()
	api := &InfoAPI{keeper: &keeper.Keeper{}}
	_, err := api.FeeHistory(context.Background(), 1, rpc.LatestBlockNumber, nil)
	require.ErrorIs(t, err, receipt.ErrNotConfigured)
}

func TestGetRewardsWithoutReceiptStore(t *testing.T) {
	t.Parallel()
	api := &InfoAPI{keeper: &keeper.Keeper{}}
	_, err := api.getRewards(nil, nil, nil)
	require.ErrorIs(t, err, receipt.ErrNotConfigured)
}
