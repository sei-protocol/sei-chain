package keeper_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestSetGetAddressMapping(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	seiAddr, evmAddr := keeper.MockAddressPair()
	foundEVM, ok := k.GetEVMAddress(ctx, seiAddr)
	require.False(t, ok)
	foundSei, ok := k.GetSeiAddress(ctx, evmAddr)
	require.False(t, ok)
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	foundEVM, ok = k.GetEVMAddress(ctx, seiAddr)
	require.True(t, ok)
	require.Equal(t, evmAddr, foundEVM)
	foundSei, ok = k.GetSeiAddress(ctx, evmAddr)
	require.True(t, ok)
	require.Equal(t, seiAddr, foundSei)
}

func TestDeleteAddressMapping(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	seiAddr, evmAddr := keeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	foundEVM, ok := k.GetEVMAddress(ctx, seiAddr)
	require.True(t, ok)
	require.Equal(t, evmAddr, foundEVM)
	foundSei, ok := k.GetSeiAddress(ctx, evmAddr)
	require.True(t, ok)
	require.Equal(t, seiAddr, foundSei)
	k.DeleteAddressMapping(ctx, seiAddr, evmAddr)
	foundEVM, ok = k.GetEVMAddress(ctx, seiAddr)
	require.False(t, ok)
	foundSei, ok = k.GetSeiAddress(ctx, evmAddr)
	require.False(t, ok)
}
