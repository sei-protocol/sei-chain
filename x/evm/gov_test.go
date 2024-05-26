package evm_test

import (
	"testing"

	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestAddERCNativePointerProposals(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, pointer1 := testkeeper.MockAddressPair()
	_, pointer2 := testkeeper.MockAddressPair()
	require.NotNil(t, evm.HandleAddERCNativePointerProposal(ctx, k, &types.AddERCNativePointerProposal{
		Token:   "test",
		Version: uint32(native.CurrentVersion - 1),
		Pointer: pointer1.Hex(),
	}))
	require.Nil(t, evm.HandleAddERCNativePointerProposal(ctx, k, &types.AddERCNativePointerProposal{
		Token:   "test",
		Version: uint32(native.CurrentVersion),
		Pointer: pointer1.Hex(),
	}))
	addr, ver, exists := k.GetERC20NativePointer(ctx, "test")
	require.True(t, exists)
	require.Equal(t, native.CurrentVersion, ver)
	require.Equal(t, addr, pointer1)
	require.Nil(t, evm.HandleAddERCNativePointerProposal(ctx, k, &types.AddERCNativePointerProposal{
		Token:   "test",
		Version: uint32(native.CurrentVersion + 1),
		Pointer: pointer2.Hex(),
	}))
	addr, ver, exists = k.GetERC20NativePointer(ctx, "test")
	require.True(t, exists)
	require.Equal(t, native.CurrentVersion+1, ver)
	require.Equal(t, addr, pointer2)
	require.Nil(t, evm.HandleAddERCNativePointerProposal(ctx, k, &types.AddERCNativePointerProposal{
		Token:   "test",
		Version: uint32(native.CurrentVersion + 1),
	}))
	addr, ver, exists = k.GetERC20NativePointer(ctx, "test")
	require.True(t, exists)
	require.Equal(t, native.CurrentVersion, ver)
	require.Equal(t, addr, pointer1)
}

func TestAddERCCW20PointerProposals(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, pointer1 := testkeeper.MockAddressPair()
	_, pointer2 := testkeeper.MockAddressPair()
	require.NotNil(t, evm.HandleAddERCCW20PointerProposal(ctx, k, &types.AddERCCW20PointerProposal{
		Pointee: "test",
		Version: uint32(cw20.CurrentVersion(ctx) - 1),
		Pointer: pointer1.Hex(),
	}))
	require.Nil(t, evm.HandleAddERCCW20PointerProposal(ctx, k, &types.AddERCCW20PointerProposal{
		Pointee: "test",
		Version: uint32(cw20.CurrentVersion(ctx)),
		Pointer: pointer1.Hex(),
	}))
	addr, ver, exists := k.GetERC20CW20Pointer(ctx, "test")
	require.True(t, exists)
	require.Equal(t, cw20.CurrentVersion(ctx), ver)
	require.Equal(t, addr, pointer1)
	require.Nil(t, evm.HandleAddERCCW20PointerProposal(ctx, k, &types.AddERCCW20PointerProposal{
		Pointee: "test",
		Version: uint32(cw20.CurrentVersion(ctx) + 1),
		Pointer: pointer2.Hex(),
	}))
	addr, ver, exists = k.GetERC20CW20Pointer(ctx, "test")
	require.True(t, exists)
	require.Equal(t, cw20.CurrentVersion(ctx)+1, ver)
	require.Equal(t, addr, pointer2)
	require.Nil(t, evm.HandleAddERCCW20PointerProposal(ctx, k, &types.AddERCCW20PointerProposal{
		Pointee: "test",
		Version: uint32(cw20.CurrentVersion(ctx) + 1),
	}))
	addr, ver, exists = k.GetERC20CW20Pointer(ctx, "test")
	require.True(t, exists)
	require.Equal(t, cw20.CurrentVersion(ctx), ver)
	require.Equal(t, addr, pointer1)
}

func TestAddERCCW721PointerProposals(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, pointer1 := testkeeper.MockAddressPair()
	_, pointer2 := testkeeper.MockAddressPair()
	require.NotNil(t, evm.HandleAddERCCW721PointerProposal(ctx, k, &types.AddERCCW721PointerProposal{
		Pointee: "test",
		Version: uint32(cw721.CurrentVersion - 1),
		Pointer: pointer1.Hex(),
	}))
	require.Nil(t, evm.HandleAddERCCW721PointerProposal(ctx, k, &types.AddERCCW721PointerProposal{
		Pointee: "test",
		Version: uint32(cw721.CurrentVersion),
		Pointer: pointer1.Hex(),
	}))
	addr, ver, exists := k.GetERC721CW721Pointer(ctx, "test")
	require.True(t, exists)
	require.Equal(t, cw721.CurrentVersion, ver)
	require.Equal(t, addr, pointer1)
	require.Nil(t, evm.HandleAddERCCW721PointerProposal(ctx, k, &types.AddERCCW721PointerProposal{
		Pointee: "test",
		Version: uint32(cw721.CurrentVersion + 1),
		Pointer: pointer2.Hex(),
	}))
	addr, ver, exists = k.GetERC721CW721Pointer(ctx, "test")
	require.True(t, exists)
	require.Equal(t, cw721.CurrentVersion+1, ver)
	require.Equal(t, addr, pointer2)
	require.Nil(t, evm.HandleAddERCCW721PointerProposal(ctx, k, &types.AddERCCW721PointerProposal{
		Pointee: "test",
		Version: uint32(cw721.CurrentVersion + 1),
	}))
	addr, ver, exists = k.GetERC721CW721Pointer(ctx, "test")
	require.True(t, exists)
	require.Equal(t, cw721.CurrentVersion, ver)
	require.Equal(t, addr, pointer1)
}

func TestAddCWERC20PointerProposals(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, pointee1 := testkeeper.MockAddressPair()
	pointer1, _ := testkeeper.MockAddressPair()
	pointer2, _ := testkeeper.MockAddressPair()
	require.NotNil(t, evm.HandleAddCWERC20PointerProposal(ctx, k, &types.AddCWERC20PointerProposal{
		Pointee: pointee1.Hex(),
		Version: uint32(erc20.CurrentVersion - 1),
		Pointer: pointer1.String(),
	}))
	require.Nil(t, evm.HandleAddCWERC20PointerProposal(ctx, k, &types.AddCWERC20PointerProposal{
		Pointee: pointee1.Hex(),
		Version: uint32(erc20.CurrentVersion),
		Pointer: pointer1.String(),
	}))
	addr, ver, exists := k.GetCW20ERC20Pointer(ctx, pointee1)
	require.True(t, exists)
	require.Equal(t, erc20.CurrentVersion, ver)
	require.Equal(t, addr, pointer1)
	require.Nil(t, evm.HandleAddCWERC20PointerProposal(ctx, k, &types.AddCWERC20PointerProposal{
		Pointee: pointee1.Hex(),
		Version: uint32(erc20.CurrentVersion + 1),
		Pointer: pointer2.String(),
	}))
	addr, ver, exists = k.GetCW20ERC20Pointer(ctx, pointee1)
	require.True(t, exists)
	require.Equal(t, erc20.CurrentVersion+1, ver)
	require.Equal(t, addr, pointer2)
	require.Nil(t, evm.HandleAddCWERC20PointerProposal(ctx, k, &types.AddCWERC20PointerProposal{
		Pointee: pointee1.Hex(),
		Version: uint32(erc20.CurrentVersion + 1),
	}))
	addr, ver, exists = k.GetCW20ERC20Pointer(ctx, pointee1)
	require.True(t, exists)
	require.Equal(t, erc20.CurrentVersion, ver)
	require.Equal(t, addr, pointer1)
}

func TestAddCWERC721PointerProposals(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, pointee1 := testkeeper.MockAddressPair()
	pointer1, _ := testkeeper.MockAddressPair()
	pointer2, _ := testkeeper.MockAddressPair()
	require.NotNil(t, evm.HandleAddCWERC721PointerProposal(ctx, k, &types.AddCWERC721PointerProposal{
		Pointee: pointee1.Hex(),
		Version: uint32(erc721.CurrentVersion - 1),
		Pointer: pointer1.String(),
	}))
	require.Nil(t, evm.HandleAddCWERC721PointerProposal(ctx, k, &types.AddCWERC721PointerProposal{
		Pointee: pointee1.Hex(),
		Version: uint32(erc721.CurrentVersion),
		Pointer: pointer1.String(),
	}))
	addr, ver, exists := k.GetCW721ERC721Pointer(ctx, pointee1)
	require.True(t, exists)
	require.Equal(t, uint16(erc721.CurrentVersion), ver)
	require.Equal(t, addr, pointer1)
	require.Nil(t, evm.HandleAddCWERC721PointerProposal(ctx, k, &types.AddCWERC721PointerProposal{
		Pointee: pointee1.Hex(),
		Version: uint32(erc721.CurrentVersion + 1),
		Pointer: pointer2.String(),
	}))
	addr, ver, exists = k.GetCW721ERC721Pointer(ctx, pointee1)
	require.True(t, exists)
	require.Equal(t, erc721.CurrentVersion+1, ver)
	require.Equal(t, addr, pointer2)
	require.Nil(t, evm.HandleAddCWERC721PointerProposal(ctx, k, &types.AddCWERC721PointerProposal{
		Pointee: pointee1.Hex(),
		Version: uint32(erc721.CurrentVersion + 1),
	}))
	addr, ver, exists = k.GetCW721ERC721Pointer(ctx, pointee1)
	require.True(t, exists)
	require.Equal(t, uint16(erc721.CurrentVersion), ver)
	require.Equal(t, addr, pointer1)
}
