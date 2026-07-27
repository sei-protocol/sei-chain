package keeper_test

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/tmhash"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/version"
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

// TestBlockHashRingMatchesHistoricalInfo checks that the ring value written from
// LastBlockId.Hash matches the hash derived from staking HistoricalInfo for the
// same height (the pre-ring BLOCKHASH source).
func TestBlockHashRingMatchesHistoricalInfo(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	k := &testApp.EvmKeeper

	const height int64 = 10
	tmHeader := tmtypes.Header{
		Version:            version.Consensus{Block: version.BlockProtocol},
		Height:             height,
		ChainID:            "test-chain",
		ValidatorsHash:     bytes.Repeat([]byte{0x01}, tmhash.Size),
		NextValidatorsHash: bytes.Repeat([]byte{0x02}, tmhash.Size),
		ConsensusHash:      bytes.Repeat([]byte{0x03}, tmhash.Size),
		AppHash:            bytes.Repeat([]byte{0x04}, tmhash.Size),
		DataHash:           bytes.Repeat([]byte{0x05}, tmhash.Size),
		EvidenceHash:       bytes.Repeat([]byte{0x06}, tmhash.Size),
		LastResultsHash:    bytes.Repeat([]byte{0x07}, tmhash.Size),
		LastCommitHash:     bytes.Repeat([]byte{0x08}, tmhash.Size),
		ProposerAddress:    bytes.Repeat([]byte{0x09}, 20),
	}
	wantHash := common.BytesToHash(tmHeader.Hash())
	require.NotEqual(t, common.Hash{}, wantHash)

	// BeginBlock at height: staking persists HistoricalInfo from the block header.
	ctx := testApp.GetContextForDeliverTx([]byte{}).
		WithBlockHeader(*tmHeader.ToProto()).
		WithBlockHeight(height)
	testApp.StakingKeeper.TrackHistoricalInfo(ctx)

	// BeginBlock at height+1: EVM ring records LastBlockId.Hash for the parent.
	next := testApp.GetContextForDeliverTx([]byte{}).BlockHeader()
	next.Height = height + 1
	next.LastBlockId = tmproto.BlockID{Hash: tmHeader.Hash()}
	ctx = ctx.WithBlockHeader(next).WithBlockHeight(height + 1)
	k.TrackBlockHash(ctx)

	ring, found := k.GetBlockHash(ctx, height)
	require.True(t, found)
	require.Equal(t, wantHash, ring)

	hi, found := testApp.StakingKeeper.GetHistoricalInfo(ctx, height)
	require.True(t, found)
	histHeader, err := tmtypes.HeaderFromProto(&hi.Header)
	require.NoError(t, err)
	histHash := common.BytesToHash(histHeader.Hash())
	require.Equal(t, wantHash, histHash)
	require.Equal(t, histHash, ring)
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
