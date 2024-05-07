package keeper_test

import (
	"testing"

	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestERC20NativePointer(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, pointer := testkeeper.MockAddressPair()
	require.Nil(t, k.SetERC20NativePointer(ctx, "test", pointer))
	require.NotNil(t, k.SetERC20NativePointer(ctx, "test", pointer)) // already set
	addr, _, _ := k.GetERC20NativePointer(ctx, "test")
	require.Equal(t, pointer, addr)
	token, _, _ := k.GetERC20NativeByPointer(ctx, addr)
	require.Equal(t, "test", token)
}

func TestSetERC20CW20Pointer(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, pointer := testkeeper.MockAddressPair()
	cw20, _ := testkeeper.MockAddressPair()
	require.Nil(t, k.SetERC20CW20Pointer(ctx, cw20.String(), pointer))
	require.NotNil(t, k.SetERC20CW20Pointer(ctx, cw20.String(), pointer)) // already set
	addr, _, _ := k.GetERC20CW20Pointer(ctx, cw20.String())
	require.Equal(t, pointer, addr)
	cw20Addr, _, _ := k.GetERC20CW20ByPointer(ctx, addr)
	require.Equal(t, cw20.String(), cw20Addr)
}

func TestERC721CW721Pointer(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, pointer := testkeeper.MockAddressPair()
	cw721, _ := testkeeper.MockAddressPair()
	require.Nil(t, k.SetERC721CW721Pointer(ctx, cw721.String(), pointer))
	require.NotNil(t, k.SetERC721CW721Pointer(ctx, cw721.String(), pointer)) // already set
	addr, _, _ := k.GetERC721CW721Pointer(ctx, cw721.String())
	require.Equal(t, pointer, addr)
	cw721Addr, _, _ := k.GetERC721CW721ByPointer(ctx, addr)
	require.Equal(t, cw721.String(), cw721Addr)
}
