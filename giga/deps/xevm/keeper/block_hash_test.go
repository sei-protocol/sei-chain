package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/giga/deps/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestPruneBlockHashCache(t *testing.T) {
	testApp, ctx := keeper.MockApp(t)
	k := &testApp.GigaEvmKeeper

	stale := common.HexToHash("0x0101010101010101010101010101010101010101010101010101010101010101")
	k.SetBlockHash(ctx, 0, stale)
	require.Equal(t, stale, k.GetHashFn(ctx)(0))
	k.DeleteBlockHash(ctx, 0)
	require.Equal(t, stale, k.GetHashFn(ctx)(0))

	ctx = ctx.WithBlockHeight(257)
	k.PruneBlockHashCache(ctx)
	require.Equal(t, common.Hash{}, k.GetHashFn(ctx)(0))
}
