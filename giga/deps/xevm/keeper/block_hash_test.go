package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/giga/deps/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestBlockHashCacheDeliverOnly(t *testing.T) {
	testApp, ctx := keeper.MockApp(t)
	k := &testApp.GigaEvmKeeper

	hash := common.HexToHash("0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
	k.SetBlockHash(ctx, 7, hash)

	traceCtx := ctx.WithTraceMode(true)
	require.Equal(t, hash, k.GetHashFn(traceCtx)(7))
	k.DeleteBlockHash(ctx, 7)
	require.Equal(t, common.Hash{}, k.GetHashFn(traceCtx)(7))

	k.SetBlockHash(ctx, 7, hash)
	require.Equal(t, hash, k.GetHashFn(ctx)(7))
	k.DeleteBlockHash(ctx, 7)
	require.Equal(t, hash, k.GetHashFn(ctx)(7))
}
