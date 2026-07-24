package receipt

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

// countdownContext reports itself canceled once its internal counter reaches
// zero, letting a test deterministically trip a ctx.Err() check buried a
// fixed number of calls deep in a loop, without racing real goroutines.
type countdownContext struct {
	context.Context
	remaining *int32
}

func newCountdownContext(callsBeforeCancel int32) *countdownContext {
	remaining := callsBeforeCancel
	return &countdownContext{Context: context.Background(), remaining: &remaining}
}

func (c *countdownContext) Err() error {
	if atomic.AddInt32(c.remaining, -1) < 0 {
		return context.Canceled
	}
	return nil
}

func setupLittCtxStore(t *testing.T) (*littReceiptStore, func()) {
	t.Helper()
	_, key := newTestContext()

	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "littidx"
	cfg.DBDirectory = t.TempDir()
	cfg.KeepRecent = 0
	store, err := NewReceiptStore(cfg, key)
	require.NoError(t, err)
	s, ok := store.(*littReceiptStore)
	require.True(t, ok)
	return s, func() { _ = s.Close() }
}

func newTestCtxAtHeight(height uint64) sdk.Context {
	ctx, _ := newTestContext()
	return ctx.WithBlockHeight(int64(height)) //nolint:gosec // small test heights
}

func littCtxTestReceipt(block uint64, txIndex uint32, addr common.Address, topic common.Hash, nLogs int) (common.Hash, *types.Receipt) {
	var txHash common.Hash
	copy(txHash[:], fmt.Sprintf("ctxtx-%d-%d", block, txIndex))
	logs := make([]*types.Log, nLogs)
	for i := range logs {
		logs[i] = &types.Log{Address: addr.Hex(), Topics: []string{topic.Hex()}, Index: uint32(i)} //nolint:gosec // small test indices
	}
	return txHash, &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      block,
		TransactionIndex: txIndex,
		Logs:             logs,
	}
}

func TestBlockLogsReturnsCanceledContextBeforeScanning(t *testing.T) {
	s, closeFn := setupLittCtxStore(t)
	defer closeFn()

	addr := common.HexToAddress("0xabc1")
	topic := common.HexToHash("0xdef1")
	txHash, rcpt := littCtxTestReceipt(1, 0, addr, topic, 1)
	require.NoError(t, s.SetReceipts(newTestCtxAtHeight(1), []ReceiptRecord{{TxHash: txHash, Receipt: rcpt}}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	groups := criteriaTagGroups(filters.FilterCriteria{Addresses: []common.Address{addr}})
	logs, err := s.blockLogs(ctx, 1, groups, filters.FilterCriteria{Addresses: []common.Address{addr}}, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, logs)
}

func TestCandidateBlockLogsReturnsCanceledContextBeforeTx(t *testing.T) {
	s, closeFn := setupLittCtxStore(t)
	defer closeFn()

	addr := common.HexToAddress("0xabc2")
	topic := common.HexToHash("0xdef2")
	txHash, rcpt := littCtxTestReceipt(2, 0, addr, topic, 1)
	require.NoError(t, s.SetReceipts(newTestCtxAtHeight(2), []ReceiptRecord{{TxHash: txHash, Receipt: rcpt}}))

	candidates, err := s.blockTagCandidates(2, criteriaTagGroups(filters.FilterCriteria{}))
	require.NoError(t, err)
	require.NotEmpty(t, candidates)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	logs, err := s.candidateBlockLogs(ctx, candidates, filters.FilterCriteria{}, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, logs)
}

// TestCandidateBlockLogsCancelsMidLoopOverLogs exercises the per-log ctx.Err()
// check added inside the inner loop over receipt.Logs: cancellation lands
// after the first log of a multi-log tx has already been scanned, so the
// function must stop before scanning the remaining logs rather than only
// checking once per transaction.
func TestCandidateBlockLogsCancelsMidLoopOverLogs(t *testing.T) {
	s, closeFn := setupLittCtxStore(t)
	defer closeFn()

	addr := common.HexToAddress("0xabc3")
	topic := common.HexToHash("0xdef3")
	txHash, rcpt := littCtxTestReceipt(3, 0, addr, topic, 5)
	require.NoError(t, s.SetReceipts(newTestCtxAtHeight(3), []ReceiptRecord{{TxHash: txHash, Receipt: rcpt}}))

	candidates, err := s.blockTagCandidates(3, criteriaTagGroups(filters.FilterCriteria{}))
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	// Calls to ctx.Err(): 1 (tx-level check) + 1 (log[0] check) both return
	// nil, then the check before log[1] trips.
	ctx := newCountdownContext(2)

	logs, err := s.candidateBlockLogs(ctx, candidates, filters.FilterCriteria{Addresses: []common.Address{addr}}, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, logs, "no logs should be returned once cancellation trips mid-scan")
}

// TestCandidateBlockLogsTripsBudgetMidLoopOverLogs exercises the per-log
// budget.Tripped() check added inside the inner loop: a receipt with 2
// matching logs and a budget capped at 1 must stop after reserving the first
// log instead of reserving both before checking.
func TestCandidateBlockLogsTripsBudgetMidLoopOverLogs(t *testing.T) {
	s, closeFn := setupLittCtxStore(t)
	defer closeFn()

	addr := common.HexToAddress("0xabc4")
	topic := common.HexToHash("0xdef4")
	txHash, rcpt := littCtxTestReceipt(4, 0, addr, topic, 2)
	require.NoError(t, s.SetReceipts(newTestCtxAtHeight(4), []ReceiptRecord{{TxHash: txHash, Receipt: rcpt}}))

	candidates, err := s.blockTagCandidates(4, criteriaTagGroups(filters.FilterCriteria{}))
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	budget := NewLogBudget(1, 0)
	logs, err := s.candidateBlockLogs(context.Background(), candidates, filters.FilterCriteria{Addresses: []common.Address{addr}}, budget)
	require.ErrorIs(t, err, ErrTooManyLogs)
	require.Nil(t, logs)
}

// TestFilterLogsByTagsPreCanceledContextReturnsEmptyFast documents that a
// context already canceled before the fan-out starts short-circuits the
// whole scan: errgroup.WithContext observes the parent as already done, so
// no per-block worker is scheduled and the call returns immediately with no
// logs and no error (mirroring the empty-range early return, not a budget
// trip).
func TestFilterLogsByTagsPreCanceledContextReturnsEmptyFast(t *testing.T) {
	s, closeFn := setupLittCtxStore(t)
	defer closeFn()

	addr := common.HexToAddress("0xabc5")
	topic := common.HexToHash("0xdef5")
	for block := uint64(1); block <= 5; block++ {
		txHash, rcpt := littCtxTestReceipt(block, 0, addr, topic, 1)
		require.NoError(t, s.SetReceipts(newTestCtxAtHeight(block), []ReceiptRecord{{TxHash: txHash, Receipt: rcpt}}))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	logs, err := s.filterLogsByTags(ctx, 1, 5, filters.FilterCriteria{Addresses: []common.Address{addr}}, nil)
	require.NoError(t, err)
	require.Empty(t, logs)
}

// TestFilterLogsThreadsSDKContext verifies the public FilterLogs entrypoint
// derives its context from the sdk.Context (via ctx.Context()) rather than
// always using context.Background(): a canceled context reaches the tag scan
// and short-circuits it, while an sdk.Context with no wrapped context.Context
// falls back to a live context and completes normally.
func TestFilterLogsThreadsSDKContext(t *testing.T) {
	s, closeFn := setupLittCtxStore(t)
	defer closeFn()

	addr := common.HexToAddress("0xabc6")
	topic := common.HexToHash("0xdef6")
	txHash, rcpt := littCtxTestReceipt(6, 0, addr, topic, 1)
	require.NoError(t, s.SetReceipts(newTestCtxAtHeight(6), []ReceiptRecord{{TxHash: txHash, Receipt: rcpt}}))

	crit := filters.FilterCriteria{Addresses: []common.Address{addr}}

	// No wrapped context.Context: FilterLogs must fall back to a usable
	// context rather than panicking or erroring.
	logs, err := s.FilterLogs(newTestCtxAtHeight(6), 6, 6, crit, nil)
	require.NoError(t, err)
	require.Len(t, logs, 1)

	// A canceled wrapped context.Context must short-circuit the scan.
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	logs, err = s.FilterLogs(newTestCtxAtHeight(6).WithContext(canceledCtx), 6, 6, crit, nil)
	require.NoError(t, err)
	require.Empty(t, logs)
}
