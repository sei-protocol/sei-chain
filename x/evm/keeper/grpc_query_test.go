package keeper_test

import (
	"errors"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw1155"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc1155"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestQueryPointer(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	seiAddr1, evmAddr1 := testkeeper.MockAddressPair()
	seiAddr2, evmAddr2 := testkeeper.MockAddressPair()
	seiAddr3, evmAddr3 := testkeeper.MockAddressPair()
	seiAddr4, evmAddr4 := testkeeper.MockAddressPair()
	seiAddr5, evmAddr5 := testkeeper.MockAddressPair()
	seiAddr6, evmAddr6 := testkeeper.MockAddressPair()
	seiAddr7, evmAddr7 := testkeeper.MockAddressPair()
	goCtx := sdk.WrapSDKContext(ctx)
	k.SetERC20NativePointer(ctx, seiAddr1.String(), evmAddr1)
	k.SetERC20CW20Pointer(ctx, seiAddr2.String(), evmAddr2)
	k.SetERC721CW721Pointer(ctx, seiAddr3.String(), evmAddr3)
	k.SetCW20ERC20Pointer(ctx, evmAddr4, seiAddr4.String())
	k.SetCW721ERC721Pointer(ctx, evmAddr5, seiAddr5.String())
	k.SetERC1155CW1155Pointer(ctx, seiAddr6.String(), evmAddr6)
	k.SetCW1155ERC1155Pointer(ctx, evmAddr7, seiAddr7.String())
	q := keeper.Querier{k}
	res, err := q.Pointer(goCtx, &types.QueryPointerRequest{PointerType: types.PointerType_NATIVE, Pointee: seiAddr1.String()})
	require.Nil(t, err)
	require.Equal(t, types.QueryPointerResponse{Pointer: evmAddr1.Hex(), Version: uint32(native.CurrentVersion), Exists: true}, *res)
	res, err = q.Pointer(goCtx, &types.QueryPointerRequest{PointerType: types.PointerType_CW20, Pointee: seiAddr2.String()})
	require.Nil(t, err)
	require.Equal(t, types.QueryPointerResponse{Pointer: evmAddr2.Hex(), Version: uint32(cw20.CurrentVersion(ctx)), Exists: true}, *res)
	res, err = q.Pointer(goCtx, &types.QueryPointerRequest{PointerType: types.PointerType_CW721, Pointee: seiAddr3.String()})
	require.Nil(t, err)
	require.Equal(t, types.QueryPointerResponse{Pointer: evmAddr3.Hex(), Version: uint32(cw721.CurrentVersion), Exists: true}, *res)
	res, err = q.Pointer(goCtx, &types.QueryPointerRequest{PointerType: types.PointerType_ERC20, Pointee: evmAddr4.Hex()})
	require.Nil(t, err)
	require.Equal(t, types.QueryPointerResponse{Pointer: seiAddr4.String(), Version: uint32(erc20.CurrentVersion), Exists: true}, *res)
	res, err = q.Pointer(goCtx, &types.QueryPointerRequest{PointerType: types.PointerType_ERC721, Pointee: evmAddr5.Hex()})
	require.Nil(t, err)
	require.Equal(t, types.QueryPointerResponse{Pointer: seiAddr5.String(), Version: uint32(erc721.CurrentVersion), Exists: true}, *res)
	res, err = q.Pointer(goCtx, &types.QueryPointerRequest{PointerType: types.PointerType_CW1155, Pointee: seiAddr6.String()})
	require.Nil(t, err)
	require.Equal(t, types.QueryPointerResponse{Pointer: evmAddr6.Hex(), Version: uint32(cw1155.CurrentVersion), Exists: true}, *res)
	res, err = q.Pointer(goCtx, &types.QueryPointerRequest{PointerType: types.PointerType_ERC1155, Pointee: evmAddr7.Hex()})
	require.Nil(t, err)
	require.Equal(t, types.QueryPointerResponse{Pointer: seiAddr7.String(), Version: uint32(erc1155.CurrentVersion), Exists: true}, *res)
}

func TestQueryPointee(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, pointerAddr1 := testkeeper.MockAddressPair()
	seiAddr2, evmAddr2 := testkeeper.MockAddressPair()
	seiAddr3, evmAddr3 := testkeeper.MockAddressPair()
	seiAddr4, evmAddr4 := testkeeper.MockAddressPair()
	seiAddr5, evmAddr5 := testkeeper.MockAddressPair()
	goCtx := sdk.WrapSDKContext(ctx)

	// Set up pointers for each type
	k.SetERC20NativePointer(ctx, "ufoo", pointerAddr1)
	k.SetERC20CW20Pointer(ctx, seiAddr2.String(), evmAddr2)
	k.SetERC721CW721Pointer(ctx, seiAddr3.String(), evmAddr3)
	k.SetCW20ERC20Pointer(ctx, evmAddr4, seiAddr4.String())
	k.SetCW721ERC721Pointer(ctx, evmAddr5, seiAddr5.String())

	q := keeper.Querier{k}

	// Test for Native Pointee
	res, err := q.Pointee(goCtx, &types.QueryPointeeRequest{PointerType: types.PointerType_NATIVE, Pointer: pointerAddr1.Hex()})
	require.Nil(t, err)
	require.Equal(t, types.QueryPointeeResponse{Pointee: "ufoo", Version: uint32(native.CurrentVersion), Exists: true}, *res)

	// Test for CW20 Pointee
	res, err = q.Pointee(goCtx, &types.QueryPointeeRequest{PointerType: types.PointerType_CW20, Pointer: evmAddr2.Hex()})
	require.Nil(t, err)
	require.Equal(t, types.QueryPointeeResponse{Pointee: seiAddr2.String(), Version: uint32(cw20.CurrentVersion(ctx)), Exists: true}, *res)

	// Test for CW721 Pointee
	res, err = q.Pointee(goCtx, &types.QueryPointeeRequest{PointerType: types.PointerType_CW721, Pointer: evmAddr3.Hex()})
	require.Nil(t, err)
	require.Equal(t, types.QueryPointeeResponse{Pointee: seiAddr3.String(), Version: uint32(cw721.CurrentVersion), Exists: true}, *res)

	// Test for ERC20 Pointee
	res, err = q.Pointee(goCtx, &types.QueryPointeeRequest{PointerType: types.PointerType_ERC20, Pointer: seiAddr4.String()})
	require.Nil(t, err)
	require.Equal(t, types.QueryPointeeResponse{Pointee: evmAddr4.Hex(), Version: uint32(erc20.CurrentVersion), Exists: true}, *res)

	// Test for ERC721 Pointee
	res, err = q.Pointee(goCtx, &types.QueryPointeeRequest{PointerType: types.PointerType_ERC721, Pointer: seiAddr5.String()})
	require.Nil(t, err)
	require.Equal(t, types.QueryPointeeResponse{Pointee: evmAddr5.Hex(), Version: uint32(erc721.CurrentVersion), Exists: true}, *res)

	// Test for not registered Native Pointee
	res, err = q.Pointee(goCtx, &types.QueryPointeeRequest{PointerType: types.PointerType_NATIVE, Pointer: "0x1234567890123456789012345678901234567890"})
	require.Nil(t, err)
	require.Equal(t, types.QueryPointeeResponse{Pointee: "", Version: 0, Exists: false}, *res)

	// Test for not registered CW20 Pointee
	res, err = q.Pointee(goCtx, &types.QueryPointeeRequest{PointerType: types.PointerType_CW20, Pointer: "0x1234567890123456789012345678901234567890"})
	require.Nil(t, err)
	require.Equal(t, types.QueryPointeeResponse{Pointee: "", Version: 0, Exists: false}, *res)

	// Test for not registered CW721 Pointee
	res, err = q.Pointee(goCtx, &types.QueryPointeeRequest{PointerType: types.PointerType_CW721, Pointer: "0x1234567890123456789012345678901234567890"})
	require.Nil(t, err)
	require.Equal(t, types.QueryPointeeResponse{Pointee: "", Version: 0, Exists: false}, *res)

	// Test for not registered ERC20 Pointee
	res, err = q.Pointee(goCtx, &types.QueryPointeeRequest{PointerType: types.PointerType_ERC20, Pointer: "sei1notregistered"})
	require.Nil(t, err)
	require.Equal(t, types.QueryPointeeResponse{Pointee: "0x0000000000000000000000000000000000000000", Version: 0, Exists: false}, *res)

	// Test for not registered ERC721 Pointee
	res, err = q.Pointee(goCtx, &types.QueryPointeeRequest{PointerType: types.PointerType_ERC721, Pointer: "sei1notregistered"})
	require.Nil(t, err)
	require.Equal(t, types.QueryPointeeResponse{Pointee: "0x0000000000000000000000000000000000000000", Version: 0, Exists: false}, *res)

	// Test cases for invalid inputs
	testCases := []struct {
		name        string
		req         *types.QueryPointeeRequest
		expectedRes *types.QueryPointeeResponse
		expectedErr error
	}{
		{
			name:        "Invalid pointer type",
			req:         &types.QueryPointeeRequest{PointerType: 999, Pointer: pointerAddr1.Hex()},
			expectedRes: nil,
			expectedErr: errors.ErrUnsupported,
		},
		{
			name:        "Empty pointer",
			req:         &types.QueryPointeeRequest{PointerType: types.PointerType_NATIVE, Pointer: ""},
			expectedRes: &types.QueryPointeeResponse{Pointee: "", Version: 0, Exists: false},
			expectedErr: nil,
		},
		{
			name:        "Invalid hex address for EVM-based pointer types",
			req:         &types.QueryPointeeRequest{PointerType: types.PointerType_CW20, Pointer: "not-a-hex-address"},
			expectedRes: &types.QueryPointeeResponse{Pointee: "", Version: 0, Exists: false},
			expectedErr: nil,
		},
		{
			name:        "Invalid bech32 address for Cosmos-based pointer types",
			req:         &types.QueryPointeeRequest{PointerType: types.PointerType_ERC20, Pointer: "not-a-bech32-address"},
			expectedRes: &types.QueryPointeeResponse{Pointee: "0x0000000000000000000000000000000000000000", Version: 0, Exists: false},
			expectedErr: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := q.Pointee(goCtx, tc.req)
			if tc.expectedErr != nil {
				require.ErrorIs(t, err, tc.expectedErr)
				require.Nil(t, res)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedRes, res)
			}
		})
	}
}
