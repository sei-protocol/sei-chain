package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetGetBalance(t *testing.T) {
	k, ctx := MockEVMKeeper()
	_, evmAddr := MockAddressPair()
	k.SetBalance(ctx, evmAddr, 10)
	require.Equal(t, uint64(10), k.GetBalance(ctx, evmAddr))
	k.SetBalance(ctx, evmAddr, 20)
	require.Equal(t, uint64(20), k.GetBalance(ctx, evmAddr))
	k.SetBalance(ctx, evmAddr, 0)
	require.Equal(t, uint64(0), k.GetBalance(ctx, evmAddr))
}
