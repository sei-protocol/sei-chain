package evmonly

import (
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// waitEarlySpeculation blocks until the early speculation of block number, if any, has finished.
func waitEarlySpeculation(e *Executor, number uint64) {
	e.earlyMu.Lock()
	s := e.early[number]
	e.earlyMu.Unlock()
	if s != nil {
		s.wg.Wait()
	}
}

func newEarlyExecutor(store *MemoryStore) *Executor {
	return NewExecutor(Config{OCCWorkers: 4, EarlySpeculationWorkers: 3},
		withTestStores(store, NewMemoryReceiptStore()))
}

// Blocks speculated early against state several blocks old must still execute to exactly what
// synchronous execution produces: every result an intervening block invalidated is re-speculated.
func TestEarlySpeculationMatchesSynchronousExecution(t *testing.T) {
	scenario := newPipelineScenario(t)
	syncStore := NewMemoryStore(scenario.genesis())
	earlyStore := NewMemoryStore(scenario.genesis())
	syncExecutor := NewExecutor(Config{OCCWorkers: 4}, withTestStores(syncStore, NewMemoryReceiptStore()))
	defer syncExecutor.Close()
	earlyExecutor := newEarlyExecutor(earlyStore)
	defer earlyExecutor.Close()

	requests := make([]BlockRequest, 0, 4)
	prepared := make([]PreparedBlock, 0, 4)
	for number := uint64(1); number <= 4; number++ {
		blockCtx := blockContext(scenario.chainID)
		blockCtx.Number = number
		req := BlockRequest{Context: blockCtx, Txs: scenario.block(t, number)}
		block, err := earlyExecutor.PrepareBlock(t.Context(), req)
		require.NoError(t, err)
		// Every block is speculated against genesis, before any block executes.
		earlyExecutor.SpeculateEarly(block)
		waitEarlySpeculation(earlyExecutor, number)
		requests = append(requests, req)
		prepared = append(prepared, block)
	}

	for i, req := range requests {
		want, err := syncExecutor.ExecuteBlock(t.Context(), req)
		require.NoError(t, err)
		got, err := earlyExecutor.ExecutePreparedBlock(t.Context(), prepared[i])
		require.NoError(t, err)
		number := req.Context.Number
		require.Equalf(t, want.Txs, got.Txs, "block %d transaction results", number)
		require.Equalf(t, want.Receipts, got.Receipts, "block %d receipts", number)
		require.Equalf(t, want.ChangeSet, got.ChangeSet, "block %d changeset", number)
		require.Equalf(t, want.GasUsed, got.GasUsed, "block %d gas", number)
		want.Release()
		got.Release()
	}
	require.NoError(t, earlyExecutor.AwaitCommits())
}

// A block speculated against the state before an executed block reuses the results that block's
// changes left alone, and re-speculates the rest.
func TestEarlySpeculationReusesOnlyResultsNoExecutedBlockChanged(t *testing.T) {
	scenario := newPipelineScenario(t)
	idle, err := crypto.GenerateKey()
	require.NoError(t, err)
	genesis := scenario.genesis()
	genesis.SetBalance(crypto.PubkeyToAddress(idle.PublicKey), big.NewInt(10*testFundedBalanceWei))
	store := NewMemoryStore(genesis)
	executor := newEarlyExecutor(store)
	defer executor.Close()

	first := blockContext(scenario.chainID)
	first.Number = 1
	block1, err := executor.PrepareBlock(t.Context(), BlockRequest{Context: first, Txs: scenario.block(t, 1)})
	require.NoError(t, err)

	second := blockContext(scenario.chainID)
	second.Number = 2
	untouched := testAddress(0x77)
	ringSender := scenario.ring[1]
	ringRecipient := crypto.PubkeyToAddress(scenario.ring[2].PublicKey)
	block2, err := executor.PrepareBlock(t.Context(), BlockRequest{Context: second, Txs: [][]byte{
		// The idle account appears in no block-1 transaction, so its speculation stays valid.
		signLegacyTx(t, idle, scenario.chainID, 0, &untouched, big.NewInt(5), nil),
		// This sender's nonce moves in block 1, so its speculation against genesis is stale.
		signLegacyTx(t, ringSender, scenario.chainID, 1, &ringRecipient, big.NewInt(6), nil),
	}})
	require.NoError(t, err)
	executor.SpeculateEarly(block2)
	waitEarlySpeculation(executor, 2)

	result, err := executor.ExecutePreparedBlock(t.Context(), block1)
	require.NoError(t, err)
	result.Release()

	results := make([]occTxExecution, len(block2.Txs))
	filled := executor.reuseEarlySpeculation(t.Context(), block2, results)
	require.Equal(t, []bool{true, false}, filled)
	require.NoError(t, executor.AwaitCommits())
}

// Early results computed for other block constants are never reused.
func TestEarlySpeculationIsDiscardedForAnotherBlockContext(t *testing.T) {
	scenario := newPipelineScenario(t)
	executor := newEarlyExecutor(NewMemoryStore(scenario.genesis()))
	defer executor.Close()

	blockCtx := blockContext(scenario.chainID)
	blockCtx.Number = 1
	block, err := executor.PrepareBlock(t.Context(), BlockRequest{Context: blockCtx, Txs: scenario.block(t, 1)})
	require.NoError(t, err)
	executor.SpeculateEarly(block)
	waitEarlySpeculation(executor, 1)

	block.Context.Time++
	results := make([]occTxExecution, len(block.Txs))
	require.Nil(t, executor.reuseEarlySpeculation(t.Context(), block, results))
}

// Taking over a speculation releases its workers still waiting for an early slot.
func TestEarlySpeculationStopsWorkersWaitingForASlot(t *testing.T) {
	scenario := newPipelineScenario(t)
	executor := newEarlyExecutor(NewMemoryStore(scenario.genesis()))
	defer executor.Close()
	for range cap(executor.earlySlots) {
		executor.earlySlots <- struct{}{}
	}

	blockCtx := blockContext(scenario.chainID)
	blockCtx.Number = 1
	block, err := executor.PrepareBlock(t.Context(), BlockRequest{Context: blockCtx, Txs: scenario.block(t, 1)})
	require.NoError(t, err)
	executor.SpeculateEarly(block)
	executor.earlyMu.Lock()
	s := executor.early[1]
	executor.earlyMu.Unlock()
	// Once every chunk is claimed, the workers holding one are waiting for a slot.
	require.Eventually(t, func() bool { return s.claimed.Load() >= int64(len(block.Txs)) }, time.Second, time.Millisecond)

	results := make([]occTxExecution, len(block.Txs))
	require.Equal(t, make([]bool, len(block.Txs)), executor.reuseEarlySpeculation(t.Context(), block, results))
}

// An early result that wrote, even without reading, state a block executed since changed is stale.
func TestEarlySpeculationTreatsAWriteToChangedStateAsStale(t *testing.T) {
	scenario := newPipelineScenario(t)
	executor := newEarlyExecutor(NewMemoryStore(scenario.genesis()))
	defer executor.Close()

	blockCtx := blockContext(scenario.chainID)
	blockCtx.Number = 2
	block, err := executor.PrepareBlock(t.Context(), BlockRequest{Context: blockCtx, Txs: scenario.block(t, 2)})
	require.NoError(t, err)
	block.Txs = block.Txs[:2]
	changed, untouched := testAddress(0x77), testAddress(0x78)
	s := &earlySpeculation{
		block:   block,
		results: make([]occTxExecution, 2),
		done:    make([]atomic.Bool, 2),
		stop:    make(chan struct{}),
	}
	for i, addr := range []common.Address{changed, untouched} {
		s.results[i].writeSet = map[stateAccessKey]struct{}{{kind: stateAccessNonce, address: addr}: {}}
		s.done[i].Store(true)
	}
	executor.early[2] = s
	executor.recentChanges[1] = newPendingIndex(&StateChangeSet{
		Accounts: []AccountChange{{Address: changed, Account: AccountState{Balance: big.NewInt(1), Nonce: 1}}},
	})

	results := make([]occTxExecution, 2)
	require.Equal(t, []bool{false, true}, executor.reuseEarlySpeculation(t.Context(), block, results))
}
