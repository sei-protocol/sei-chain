package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/app"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

func TestTrackBlockHash(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	k := &testApp.EvmKeeper

	parentHash := common.HexToHash("0xaabbccddeeff00112233445566778899aabbccddeeff00112233445566778899")
	header := testApp.GetContextForDeliverTx([]byte{}).BlockHeader()
	header.Height = 10
	header.LastBlockId = tmproto.BlockID{Hash: parentHash.Bytes()}
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeader(header)

	k.TrackBlockHash(ctx)
	got, found := k.GetBlockHash(ctx, 9)
	require.True(t, found)
	require.Equal(t, parentHash, got)

	// At height 257, prune height 0 out of the window.
	stale := common.HexToHash("0x0101010101010101010101010101010101010101010101010101010101010101")
	k.SetBlockHash(ctx, 0, stale)
	require.Equal(t, stale, k.GetHashFn(ctx)(0))

	nextParent := common.HexToHash("0x0202020202020202020202020202020202020202020202020202020202020202")
	header.Height = 257
	header.LastBlockId = tmproto.BlockID{Hash: nextParent.Bytes()}
	ctx = ctx.WithBlockHeader(header)
	k.TrackBlockHash(ctx)

	_, found = k.GetBlockHash(ctx, 0)
	require.False(t, found)
	parent, found := k.GetBlockHash(ctx, 256)
	require.True(t, found)
	require.Equal(t, nextParent, parent)
	require.Equal(t, common.Hash{}, k.GetHashFn(ctx)(0))
}
