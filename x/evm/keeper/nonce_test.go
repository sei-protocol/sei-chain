package keeper_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestNonce(t *testing.T) {
	k, ctx := keepertest.MockEVMKeeper()
	_, evmAddr := keepertest.MockAddressPair()
	require.Equal(t, uint64(0), k.GetNonce(ctx, evmAddr))
	k.SetNonce(ctx, evmAddr, 1)
	require.Equal(t, uint64(1), k.GetNonce(ctx, evmAddr))
}
