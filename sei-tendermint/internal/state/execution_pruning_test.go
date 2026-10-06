package state_test

import (
	"fmt"
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

var pruningStart = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func pruningTime(height int64) time.Time {
	return pruningStart.Add(time.Duration(height) * time.Hour)
}

func pruningState(params types.EvidenceParams) sm.State {
	return sm.State{
		LastBlockHeight: pruningHeadHeight,
		LastBlockTime:   pruningTime(pruningHeadHeight),
		ConsensusParams: types.ConsensusParams{Evidence: params},
	}
}

func pruningBlockStore(t *testing.T) *store.BlockStore {
	t.Helper()

	blockStore := store.NewBlockStore(dbm.NewMemDB())
	for height := int64(1); height <= pruningHeadHeight; height++ {
		block := types.MakeBlock(height, nil, &types.Commit{Height: height - 1}, nil)
		block.Header.Time = pruningTime(height)
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

func hourlyMetaStore(base int64) *mocks.BlockStore {
	blockStore := &mocks.BlockStore{}
	blockStore.On("Base").Return(base)
	blockStore.On("LoadBlockMeta", mock.AnythingOfType("int64")).Return(func(height int64) *types.BlockMeta {
		return &types.BlockMeta{Header: types.Header{Time: pruningTime(height)}}
	})
	return blockStore
}

func TestPrunableHeight(t *testing.T) {
	blockExec := pruningBlockExecutor(t, &mocks.Store{}, pruningBlockStore(t))

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
			got, err := blockExec.PrunableHeight(pruningState(tc.params), tc.requested)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestPrunableHeightFromBase(t *testing.T) {
	state := pruningState(types.EvidenceParams{MaxAgeNumBlocks: 10, MaxAgeDuration: 120 * time.Hour})

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
			blockExec := pruningBlockExecutor(t, &mocks.Store{}, hourlyMetaStore(tc.base))
			got, err := blockExec.PrunableHeight(state, 101)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestPrunableHeightHitsHeightBoundOnFastPath(t *testing.T) {
	state := pruningState(types.EvidenceParams{MaxAgeNumBlocks: 10, MaxAgeDuration: 9 * time.Hour})
	const heightBound int64 = 190

	for _, base := range []int64{heightBound - 1, heightBound} {
		t.Run(fmt.Sprintf("base=%d", base), func(t *testing.T) {
			blockExec := pruningBlockExecutor(t, &mocks.Store{}, hourlyMetaStore(base))
			got, err := blockExec.PrunableHeight(state, heightBound+1)
			require.NoError(t, err)
			require.Equal(t, heightBound, got)
		})
	}
}

func TestPrunableHeightMissingMetadata(t *testing.T) {
	blockStore := &mocks.BlockStore{}
	blockStore.On("Base").Return(int64(1))
	blockStore.On("LoadBlockMeta", mock.AnythingOfType("int64")).Return((*types.BlockMeta)(nil))
	blockExec := pruningBlockExecutor(t, &mocks.Store{}, blockStore)

	got, err := blockExec.PrunableHeight(pruningState(types.EvidenceParams{
		MaxAgeNumBlocks: 100,
		MaxAgeDuration:  time.Hour,
	}), 101)
	require.Error(t, err)
	require.Zero(t, got)
}

func TestPruneBlocksNoopsWhenClampedToBase(t *testing.T) {
	blockStore := pruningBlockStore(t)
	stateStore := &mocks.Store{}
	blockExec := pruningBlockExecutor(t, stateStore, blockStore)

	retainHeight, err := blockExec.PrunableHeight(pruningState(types.EvidenceParams{
		MaxAgeNumBlocks: 100,
		MaxAgeDuration:  500 * time.Hour,
	}), 101)
	require.NoError(t, err)
	pruned, err := blockExec.PruneBlocks(retainHeight)
	require.NoError(t, err)
	require.Equal(t, int64(1), retainHeight)
	require.Zero(t, pruned)
	require.NotNil(t, blockStore.LoadBlockMeta(1))
	stateStore.AssertNotCalled(t, "PruneStates", mock.AnythingOfType("int64"))
}

func TestPruneBlocksUsesClampedHeight(t *testing.T) {
	blockStore := pruningBlockStore(t)
	stateStore := &mocks.Store{}
	stateStore.On("PruneStates", mock.AnythingOfType("int64")).Return(nil)
	blockExec := pruningBlockExecutor(t, stateStore, blockStore)

	retainHeight, err := blockExec.PrunableHeight(pruningState(types.EvidenceParams{
		MaxAgeNumBlocks: 100,
		MaxAgeDuration:  10 * time.Hour,
	}), 101)
	require.NoError(t, err)
	pruned, err := blockExec.PruneBlocks(retainHeight)
	require.NoError(t, err)
	require.Equal(t, int64(100), retainHeight)
	require.Equal(t, uint64(99), pruned)
	require.Nil(t, blockStore.LoadBlockMeta(99))
	require.NotNil(t, blockStore.LoadBlockMeta(100))
	stateStore.AssertCalled(t, "PruneStates", retainHeight)
}
