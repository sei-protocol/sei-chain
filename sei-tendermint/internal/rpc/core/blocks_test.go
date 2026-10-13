package core

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	dbm "github.com/tendermint/tm-db"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/mocks"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestBlockchainInfo(t *testing.T) {
	cases := []struct {
		min, max     int64
		base, height int64
		limit        int64
		resultLength int64
		wantErr      bool
	}{

		// min > max
		{0, 0, 0, 0, 10, 0, true},  // min set to 1
		{0, 1, 0, 0, 10, 0, true},  // max set to height (0)
		{0, 0, 0, 1, 10, 1, false}, // max set to height (1)
		{2, 0, 0, 1, 10, 0, true},  // max set to height (1)
		{2, 1, 0, 5, 10, 0, true},

		// negative
		{1, 10, 0, 14, 10, 10, false}, // control
		{-1, 10, 0, 14, 10, 0, true},
		{1, -10, 0, 14, 10, 0, true},
		{-9223372036854775808, -9223372036854775788, 0, 100, 20, 0, true},

		// check base
		{1, 1, 1, 1, 1, 1, false},
		{2, 5, 3, 5, 5, 3, false},

		// check limit and height
		{1, 1, 0, 1, 10, 1, false},
		{1, 1, 0, 5, 10, 1, false},
		{2, 2, 0, 5, 10, 1, false},
		{1, 2, 0, 5, 10, 2, false},
		{1, 5, 0, 1, 10, 1, false},
		{1, 5, 0, 10, 10, 5, false},
		{1, 15, 0, 10, 10, 10, false},
		{1, 15, 0, 15, 10, 10, false},
		{1, 15, 0, 15, 20, 15, false},
		{1, 20, 0, 15, 20, 15, false},
		{1, 20, 0, 20, 20, 20, false},
	}

	for i, c := range cases {
		caseString := fmt.Sprintf("test %d failed", i)
		min, max, err := filterMinMax(c.base, c.height, c.min, c.max, c.limit)
		if c.wantErr {
			require.Error(t, err, caseString)
		} else {
			require.NoError(t, err, caseString)
			require.Equal(t, 1+max-min, c.resultLength, caseString)
		}
	}
}

func TestBlockResults(t *testing.T) {
	results := &abci.ResponseFinalizeBlock{
		TxResults: []*abci.ExecTxResult{
			{Code: 0, Data: []byte{0x01}, Log: "ok", GasUsed: 10},
			{Code: 0, Data: []byte{0x02}, Log: "ok", GasUsed: 5},
			{Code: 1, Log: "not ok", GasUsed: 0},
		},
	}

	env := &Environment{}
	env.StateStore = sm.NewStore(dbm.NewMemDB())
	err := env.StateStore.SaveFinalizeBlockResponses(100, results)
	require.NoError(t, err)
	mockstore := &mocks.BlockStore{}
	mockstore.On("Height").Return(int64(100))
	mockstore.On("Base").Return(int64(1))
	env.BlockStore = mockstore

	testCases := []struct {
		height  int64
		wantErr bool
		wantRes *coretypes.ResultBlockResults
	}{
		{-1, true, nil},
		{0, true, nil},
		{101, true, nil},
		{100, false, &coretypes.ResultBlockResults{
			Height:                100,
			TxsResults:            results.TxResults,
			TotalGasUsed:          15,
			FinalizeBlockEvents:   results.Events,
			ValidatorUpdates:      results.ValidatorUpdates,
			ConsensusParamUpdates: results.ConsensusParamUpdates,
		}},
	}

	ctx := t.Context()
	for _, tc := range testCases {
		res, err := env.BlockResults(ctx, &coretypes.RequestBlockInfo{
			Height: (*coretypes.Int64)(&tc.height),
		})
		if tc.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
			assert.Equal(t, tc.wantRes, res)
		}
	}
}

// A height whose metadata is stored but whose block does not load (a missing part) has no
// hash, as Block answers a nil block for it; a height whose block loads has its meta hash.
func TestBlockHashRequiresLoadableBlock(t *testing.T) {
	hash := []byte("0123456789abcdef0123456789abcdef")
	meta := &types.BlockMeta{BlockID: types.BlockID{Hash: hash}}
	mockstore := &mocks.BlockStore{}
	mockstore.On("Height").Return(int64(10))
	mockstore.On("Base").Return(int64(1))
	mockstore.On("LoadBlockMeta", int64(9)).Return(meta)
	mockstore.On("LoadBlock", int64(9)).Return((*types.Block)(nil))
	mockstore.On("LoadBlockMeta", int64(10)).Return(meta)
	mockstore.On("LoadBlock", int64(10)).Return(&types.Block{})
	mockstore.On("LoadBlockMeta", int64(8)).Return((*types.BlockMeta)(nil))
	env := &Environment{BlockStore: mockstore}
	ctx := t.Context()

	for _, h := range []int64{8, 9} {
		height := coretypes.Int64(h)
		got, err := env.BlockHash(ctx, &coretypes.RequestBlockInfo{Height: &height})
		require.NoError(t, err, "height %d", h)
		require.Nil(t, got, "height %d", h)
		block, err := env.Block(ctx, &coretypes.RequestBlockInfo{Height: &height})
		require.NoError(t, err, "height %d", h)
		require.Nil(t, block.Block, "height %d", h)
	}

	height := coretypes.Int64(10)
	got, err := env.BlockHash(ctx, &coretypes.RequestBlockInfo{Height: &height})
	require.NoError(t, err)
	require.Equal(t, meta.BlockID.Hash, got)

	height = 11
	_, err = env.BlockHash(ctx, &coretypes.RequestBlockInfo{Height: &height})
	require.ErrorIs(t, err, coretypes.ErrHeightExceedsChainHead)
}
