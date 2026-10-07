package node

import (
	"context"
	"testing"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

func preparedHeights(app *MockApp) []int64 {
	for queue := range app.prepared.Lock() {
		var heights []int64
		for _, b := range *queue {
			heights = append(heights, b.height)
		}
		return heights
	}
	panic("unreachable")
}

func TestMockAppFinalizeBlockUsesPreparedTxs(t *testing.T) {
	key, err := ethcrypto.GenerateKey()
	require.NoError(t, err)
	tx0, _, addr := buildFastCheckTxBytesForKey(t, key, 0, 21_000)
	app := NewMockApp(abci.BaseApplication{})
	_, err = app.InitChain(&abci.RequestInitChain{InitialHeight: 1})
	require.NoError(t, err)

	prepared := &abci.RequestFinalizeBlock{Txs: [][]byte{tx0}, Header: &tmproto.Header{Height: 1}, Hash: []byte("h1")}
	require.NoError(t, app.PrepareBlock(t.Context(), prepared))
	require.Equal(t, []int64{1}, preparedHeights(app))

	// The request carries no transactions, so a result can only come from the prepared parse.
	res, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{Header: &tmproto.Header{Height: 1}, Hash: []byte("h1")})
	require.NoError(t, err)
	require.Len(t, res.TxResults, 1)
	require.Equal(t, abci.CodeTypeOK, res.TxResults[0].Code)
	require.Equal(t, uint64(1), app.EvmNonce(addr))
	require.Empty(t, preparedHeights(app))
}

func TestMockAppFinalizeBlockParsesWhenPreparedHashDiffers(t *testing.T) {
	key, err := ethcrypto.GenerateKey()
	require.NoError(t, err)
	tx0, _, _ := buildFastCheckTxBytesForKey(t, key, 0, 21_000)
	app := NewMockApp(abci.BaseApplication{})
	_, err = app.InitChain(&abci.RequestInitChain{InitialHeight: 1})
	require.NoError(t, err)

	require.NoError(t, app.PrepareBlock(t.Context(), &abci.RequestFinalizeBlock{
		Txs: [][]byte{tx0}, Header: &tmproto.Header{Height: 1}, Hash: []byte("other"),
	}))
	res, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{Header: &tmproto.Header{Height: 1}, Hash: []byte("h1")})
	require.NoError(t, err)
	require.Empty(t, res.TxResults)
}

func TestMockAppPrepareBlockKeepsThreeUpcomingBlocks(t *testing.T) {
	app := NewMockApp(abci.BaseApplication{})
	_, err := app.InitChain(&abci.RequestInitChain{InitialHeight: 5})
	require.NoError(t, err)

	for _, h := range []int64{4, 5, 6, 7, 8} {
		require.NoError(t, app.PrepareBlock(t.Context(), &abci.RequestFinalizeBlock{Header: &tmproto.Header{Height: h}}))
	}
	// Height 4 is below the next block, and height 8 exceeds the bound of three.
	require.Equal(t, []int64{5, 6, 7}, preparedHeights(app))
}

func TestMockAppFinalizeBlockStopsWaitingWhenContextEnds(t *testing.T) {
	app := NewMockApp(abci.BaseApplication{})
	_, err := app.InitChain(&abci.RequestInitChain{InitialHeight: 1})
	require.NoError(t, err)
	// A parse that never finishes stands in for one still running.
	for queue := range app.prepared.Lock() {
		*queue = append(*queue, &mockAppPreparedBlock{height: 1, hash: []byte("h1"), done: make(chan struct{})})
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = app.FinalizeBlock(ctx, &abci.RequestFinalizeBlock{Header: &tmproto.Header{Height: 1}, Hash: []byte("h1")})
	require.ErrorIs(t, err, context.Canceled)
}
