package keeper_test

import (
	"bytes"
	"testing"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestSetGetAddressMapping(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	seiAddr, evmAddr := keeper.MockAddressPair()
	_, ok := k.GetEVMAddress(ctx, seiAddr)
	require.False(t, ok)
	_, ok = k.GetSeiAddress(ctx, evmAddr)
	require.False(t, ok)
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	foundEVM, ok := k.GetEVMAddress(ctx, seiAddr)
	require.True(t, ok)
	require.Equal(t, evmAddr, foundEVM)
	foundSei, ok := k.GetSeiAddress(ctx, evmAddr)
	require.True(t, ok)
	require.Equal(t, seiAddr, foundSei)
	require.Equal(t, seiAddr, k.AccountKeeper().GetAccount(ctx, seiAddr).GetAddress())
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
	_, ok = k.GetEVMAddress(ctx, seiAddr)
	require.False(t, ok)
	_, ok = k.GetSeiAddress(ctx, evmAddr)
	require.False(t, ok)
}

func TestGetAddressOrDefault(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	seiAddr, evmAddr := keeper.MockAddressPair()
	defaultEvmAddr := k.GetEVMAddressOrDefault(ctx, seiAddr)
	require.True(t, bytes.Equal(seiAddr, defaultEvmAddr[:]))
	defaultSeiAddr := k.GetSeiAddressOrDefault(ctx, evmAddr)
	require.True(t, bytes.Equal(defaultSeiAddr, evmAddr[:]))
}

func TestGetEVMAddressForCW(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	cwAddr := wasmkeeper.BuildContractAddress(123, 456)
	cwEvmAddr, associated := k.GetEVMAddress(ctx, cwAddr)
	require.True(t, associated)
	require.Equal(t, common.BytesToAddress(cwAddr), cwEvmAddr)
}
