package keeper_test

import (
	"testing"

	testkeeper "github.com/sei-protocol/sei-chain/giga/deps/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestNonce(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	_, evmAddr := testkeeper.MockAddressPair()
	require.Equal(t, uint64(0), k.GetNonce(ctx, evmAddr))
	k.SetNonce(ctx, evmAddr, 1)
	require.Equal(t, uint64(1), k.GetNonce(ctx, evmAddr))
}
