package keeper_test

import (
	"testing"

	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestSetPointer(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, pointer := testkeeper.MockAddressPair()
	cw20, _ := testkeeper.MockAddressPair()
	cw721, _ := testkeeper.MockAddressPair()
	require.Nil(t, k.SetERC20NativePointer(ctx, "test", pointer))
	require.NotNil(t, k.SetERC20NativePointer(ctx, "test", pointer)) // already set
	require.Nil(t, k.SetERC20CW20Pointer(ctx, cw20.String(), pointer))
	require.NotNil(t, k.SetERC20CW20Pointer(ctx, cw20.String(), pointer)) // already set
	require.Nil(t, k.SetERC721CW721Pointer(ctx, cw721.String(), pointer))
	require.NotNil(t, k.SetERC721CW721Pointer(ctx, cw721.String(), pointer)) // already set
}
