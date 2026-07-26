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

	stale := common.HexToHash("0x0101010101010101010101010101010101010101010101010101010101010101")
	k.SetBlockHash(ctx, 0, stale)

	nextParent := common.HexToHash("0x0202020202020202020202020202020202020202020202020202020202020202")
	header.Height = 257
	header.LastBlockId = tmproto.BlockID{Hash: nextParent.Bytes()}
	ctx = ctx.WithBlockHeader(header)
	k.TrackBlockHash(ctx)

	_, found = k.GetBlockHash(ctx, 0)
	require.False(t, found)
	_, found = k.GetBlockHash(ctx, 9)
	require.True(t, found)
	parent, found := k.GetBlockHash(ctx, 256)
	require.True(t, found)
	require.Equal(t, nextParent, parent)
}

func TestBlockHashCacheDeliverOnly(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	k := &testApp.EvmKeeper
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(8)

	hash := common.HexToHash("0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
	k.SetBlockHash(ctx, 7, hash)

	// RPC/trace may read store but must not insert into the process cache.
	traceCtx := ctx.WithTraceMode(true)
	require.Equal(t, hash, k.GetHashFn(traceCtx)(7))
	k.DeleteBlockHash(ctx, 7)
	require.Equal(t, common.Hash{}, k.GetHashFn(traceCtx)(7))

	// DeliverTx populates the cache.
	k.SetBlockHash(ctx, 7, hash)
	require.Equal(t, hash, k.GetHashFn(ctx)(7))
	k.DeleteBlockHash(ctx, 7)
	require.Equal(t, hash, k.GetHashFn(ctx)(7))
}
