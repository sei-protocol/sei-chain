package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestSetGetAddressMapping(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	seiAddr, evmAddr := keeper.MockAddressPair()
	eaddr := k.GetEVMAddress(ctx, seiAddr)
	require.Equal(t, common.BytesToAddress(seiAddr), eaddr)
	saddr := k.GetSeiAddress(ctx, evmAddr)
	require.Equal(t, sdk.AccAddress(evmAddr[:]), saddr)
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	foundEVM := k.GetEVMAddress(ctx, seiAddr)
	require.Equal(t, evmAddr, foundEVM)
	foundSei := k.GetSeiAddress(ctx, evmAddr)
	require.Equal(t, seiAddr, foundSei)
	require.Equal(t, seiAddr, k.AccountKeeper().GetAccount(ctx, seiAddr).GetAddress())
}

func TestDeleteAddressMapping(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	seiAddr, evmAddr := keeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	foundEVM := k.GetEVMAddress(ctx, seiAddr)
	require.Equal(t, evmAddr, foundEVM)
	foundSei := k.GetSeiAddress(ctx, evmAddr)
	require.Equal(t, seiAddr, foundSei)
	k.DeleteAddressMapping(ctx, seiAddr, evmAddr)
	eaddr := k.GetEVMAddress(ctx, seiAddr)
	require.Equal(t, common.BytesToAddress(seiAddr), eaddr)
	saddr := k.GetSeiAddress(ctx, evmAddr)
	require.Equal(t, sdk.AccAddress(evmAddr[:]), saddr)
}
