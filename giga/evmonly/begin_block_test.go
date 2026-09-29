package evmonly

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

var errBeginBlockRefused = errors.New("begin block refused")

// stoppingBeginBlocker is recordingEndBlocker plus a check that refuses every
// block once stopAt blocks have ended.
type stoppingBeginBlocker struct {
	recordingEndBlocker
	stopAt int64
}

func (c stoppingBeginBlocker) BeginBlock(_ precompiles.BlockContext, state precompiles.StateReader) error {
	if c.stopAt > 0 && state.GetState(endBlockerAddr, endBlockBlocksSlot).Big().Int64() >= c.stopAt {
		return errBeginBlockRefused
	}
	return nil
}

func TestBeginBlockerErrorFailsTheBlock(t *testing.T) {
	for _, occ := range []bool{false, true} {
		t.Run(map[bool]string{false: "sequential", true: "occ"}[occ], func(t *testing.T) {
			state := NewMemoryState()
			callers := newCounterCallers(t, 2, state)
			registry := endBlockRegistry{endBlockerAddr: stoppingBeginBlocker{stopAt: 1}}
			state.SetState(endBlockerAddr, endBlockBlocksSlot, common.BigToHash(big.NewInt(1)))
			cfg := Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: registry}
			if occ {
				cfg.OCCWorkers = 4
			}
			executor := NewExecutor(cfg, withTestState(state))
			t.Cleanup(executor.Close)
			for _, txs := range [][][]byte{nil, {endBlockerTx(t, callers[0], counterOwn), endBlockerTx(t, callers[1], counterOwn)}} {
				_, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(big.NewInt(testChainID)), Txs: txs})
				require.ErrorIs(t, err, errBeginBlockRefused)
			}
		})
	}
}

func TestBeginBlockerSeesThePreviousBlockBeforeItsCommitLands(t *testing.T) {
	state := NewMemoryState()
	callers := newCounterCallers(t, 3, state)
	store := NewMemoryStore(state)
	newExecutor := func(stopAt int64) *Executor {
		executor := NewExecutor(
			Config{MinGasPrice: big.NewInt(0), OCCWorkers: 4, CustomPrecompiles: endBlockRegistry{endBlockerAddr: stoppingBeginBlocker{stopAt: stopAt}}},
			withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet),
		)
		t.Cleanup(executor.Close)
		return executor
	}
	executor := newExecutor(2)
	for number := uint64(1); number <= 2; number++ {
		executePipelinedBlock(t, executor, big.NewInt(testChainID), number, endBlockerTx(t, callers[number-1], counterShared))
	}

	// Block 2's commit may still be in flight, and block 3 is refused on its end-block write.
	blockCtx := blockContext(big.NewInt(testChainID))
	blockCtx.Number = 3
	for range 2 {
		prepared, err := executor.PrepareBlock(t.Context(), BlockRequest{Context: blockCtx, Txs: [][]byte{endBlockerTx(t, callers[2], counterShared)}})
		require.NoError(t, err)
		_, err = executor.ExecutePreparedBlock(t.Context(), prepared)
		require.ErrorIs(t, err, errBeginBlockRefused)
	}
	require.NoError(t, executor.AwaitCommits())
	view := store.OpenView()
	require.Equal(t, big.NewInt(2), view.GetStorage(endBlockerAddr, endBlockBlocksSlot).Big())
	require.Equal(t, big.NewInt(2), view.GetStorage(endBlockerAddr, counterSharedSlot).Big())
	view.Close()

	resumed := newExecutor(0)
	executePipelinedBlock(t, resumed, big.NewInt(testChainID), 3, endBlockerTx(t, callers[2], counterShared))
	require.NoError(t, resumed.AwaitCommits())
	view = store.OpenView()
	defer view.Close()
	require.Equal(t, big.NewInt(3), view.GetStorage(endBlockerAddr, endBlockBlocksSlot).Big())
	require.Equal(t, big.NewInt(3), view.GetStorage(endBlockerAddr, counterSharedSlot).Big())
}

func TestCustomBeginBlockersSkipsBuiltinsAndContractsWithoutBeginBlock(t *testing.T) {
	registry := endBlockRegistry{
		common.BytesToAddress([]byte{0x02}): stoppingBeginBlocker{},
		endBlockerLaterAddr:                 recordingEndBlocker{},
		endBlockerAddr:                      stoppingBeginBlocker{},
		testAddress(0xee):                   nil,
	}
	blockers := customBeginBlockers(registry)
	require.Len(t, blockers, 1)
	require.Equal(t, endBlockerAddr, blockers[0].address)
	require.Empty(t, customBeginBlockers(nil))
}
