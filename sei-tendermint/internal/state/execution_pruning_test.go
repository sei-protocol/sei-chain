package state_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/mocks"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const pruningHeadHeight = int64(200)

// pruningBlockStore fills a block store with one block per hour so that a
// retention cutoff can be expressed either as a height or as a time.
func pruningBlockStore(t *testing.T, startTime time.Time) *store.BlockStore {
	t.Helper()

	blockStore := store.NewBlockStore(dbm.NewMemDB())
	for height := int64(1); height <= pruningHeadHeight; height++ {
		block := types.MakeBlock(height, nil, &types.Commit{Height: height - 1}, nil)
		block.Header.Time = startTime.Add(time.Duration(height) * time.Hour)
		block.Header.ProposerAddress = make([]byte, crypto.AddressSize)
		parts, err := block.MakePartSet(testPartSize)
		require.NoError(t, err)
		blockStore.SaveBlock(block, parts, &types.Commit{Height: height})
	}
	return blockStore
}

func pruningBlockExecutor(t *testing.T, stateStore sm.Store, blockStore sm.BlockStore) *sm.BlockExecutor {
	t.Helper()

	eventBus := eventbus.NewDefault()
	require.NoError(t, eventBus.Start(t.Context()))
	proxyApp := proxy.New(&testApp{})

	return sm.NewBlockExecutor(
		stateStore,
		proxyApp,
		makeTxMempool(t, proxyApp),
		sm.EmptyEvidencePool{},
		blockStore,
		eventBus,
		types.DefaultConsensusPolicy(),
	)
}

func TestPrunableHeight(t *testing.T) {
	startTime := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	headTime := startTime.Add(time.Duration(pruningHeadHeight) * time.Hour)
	blockExec := pruningBlockExecutor(t, &mocks.Store{}, pruningBlockStore(t, startTime))

	testCases := map[string]struct {
		params    types.EvidenceParams
		requested int64
		want      int64
	}{
		"duration bound keeps metadata beyond the request": {
			params:    types.EvidenceParams{MaxAgeNumBlocks: 100, MaxAgeDuration: 500 * time.Hour},
			requested: 101,
			want:      1,
		},
		"block bound is limiting": {
			params:    types.EvidenceParams{MaxAgeNumBlocks: 100, MaxAgeDuration: 10 * time.Hour},
			requested: 101,
			want:      100,
		},
		"time bound is limiting": {
			params:    types.EvidenceParams{MaxAgeNumBlocks: 10, MaxAgeDuration: 120 * time.Hour},
			requested: 101,
			want:      80,
		},
		"request below the window is not widened": {
			params:    types.EvidenceParams{MaxAgeNumBlocks: 100, MaxAgeDuration: 10 * time.Hour},
			requested: 50,
			want:      50,
		},
		"pruning disabled by the application stays disabled": {
			params:    types.EvidenceParams{MaxAgeNumBlocks: 100, MaxAgeDuration: 10 * time.Hour},
			requested: 0,
			want:      0,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			state := sm.State{
				LastBlockHeight: pruningHeadHeight,
				LastBlockTime:   headTime,
				ConsensusParams: types.ConsensusParams{Evidence: tc.params},
			}

			got, err := blockExec.PrunableHeight(state, tc.requested)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestPrunableHeightFromBase(t *testing.T) {
	startTime := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	state := sm.State{
		LastBlockHeight: pruningHeadHeight,
		LastBlockTime:   startTime.Add(time.Duration(pruningHeadHeight) * time.Hour),
		ConsensusParams: types.ConsensusParams{
			Evidence: types.EvidenceParams{
				MaxAgeNumBlocks: 10,
				MaxAgeDuration:  120 * time.Hour,
			},
		},
	}

	testCases := map[string]struct {
		base int64
		want int64
	}{
		"base at the time cutoff":         {base: 80, want: 80},
		"base one height below":           {base: 79, want: 80},
		"base far below the time cutoff":  {base: 5, want: 80},
		"base above the time cutoff":      {base: 100, want: 100},
		"request below base is unchanged": {base: 195, want: 101},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			blockStore := &mocks.BlockStore{}
			blockStore.On("Base").Return(tc.base)
			blockStore.On("LoadBlockMeta", mock.AnythingOfType("int64")).Return(func(height int64) *types.BlockMeta {
				return &types.BlockMeta{Header: types.Header{
					Time: startTime.Add(time.Duration(height) * time.Hour),
				}}
			})
			blockExec := pruningBlockExecutor(t, &mocks.Store{}, blockStore)

			got, err := blockExec.PrunableHeight(state, 101)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestPrunableHeightMissingMetadata(t *testing.T) {
	blockStore := &mocks.BlockStore{}
	blockStore.On("Base").Return(int64(1))
	blockStore.On("LoadBlockMeta", mock.AnythingOfType("int64")).Return((*types.BlockMeta)(nil))
	blockExec := pruningBlockExecutor(t, &mocks.Store{}, blockStore)

	got, err := blockExec.PrunableHeight(sm.State{
		LastBlockHeight: pruningHeadHeight,
		LastBlockTime:   time.Now(),
		ConsensusParams: types.ConsensusParams{
			Evidence: types.EvidenceParams{
				MaxAgeNumBlocks: 100,
				MaxAgeDuration:  time.Hour,
			},
		},
	}, 101)
	require.Error(t, err)
	require.Zero(t, got)
}

func TestPruneBlocksNoopsWhenClampedToBase(t *testing.T) {
	startTime := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	blockStore := pruningBlockStore(t, startTime)
	stateStore := &mocks.Store{}
	blockExec := pruningBlockExecutor(t, stateStore, blockStore)
	state := sm.State{
		LastBlockHeight: pruningHeadHeight,
		LastBlockTime:   startTime.Add(time.Duration(pruningHeadHeight) * time.Hour),
		ConsensusParams: types.ConsensusParams{
			Evidence: types.EvidenceParams{MaxAgeNumBlocks: 100, MaxAgeDuration: 500 * time.Hour},
		},
	}

	retainHeight, err := blockExec.PrunableHeight(state, 101)
	require.NoError(t, err)
	pruned, err := blockExec.PruneBlocks(retainHeight)
	require.NoError(t, err)
	require.Equal(t, int64(1), retainHeight)
	require.Zero(t, pruned)
	require.NotNil(t, blockStore.LoadBlockMeta(1))
	stateStore.AssertNotCalled(t, "PruneStates", mock.AnythingOfType("int64"))
}

func TestPruneBlocksUsesClampedHeight(t *testing.T) {
	startTime := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	blockStore := pruningBlockStore(t, startTime)
	stateStore := &mocks.Store{}
	stateStore.On("PruneStates", mock.AnythingOfType("int64")).Return(nil)
	blockExec := pruningBlockExecutor(t, stateStore, blockStore)
	state := sm.State{
		LastBlockHeight: pruningHeadHeight,
		LastBlockTime:   startTime.Add(time.Duration(pruningHeadHeight) * time.Hour),
		ConsensusParams: types.ConsensusParams{
			Evidence: types.EvidenceParams{MaxAgeNumBlocks: 100, MaxAgeDuration: 10 * time.Hour},
		},
	}

	retainHeight, err := blockExec.PrunableHeight(state, 101)
	require.NoError(t, err)
	pruned, err := blockExec.PruneBlocks(retainHeight)
	require.NoError(t, err)
	require.Equal(t, int64(100), retainHeight)
	require.Equal(t, uint64(99), pruned)
	require.Nil(t, blockStore.LoadBlockMeta(99))
	require.NotNil(t, blockStore.LoadBlockMeta(100))
	stateStore.AssertCalled(t, "PruneStates", retainHeight)
}
