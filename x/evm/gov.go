package evm

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func HandleAddERCNativePointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddERCNativePointerProposal) error {
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypePointerRegistered, sdk.NewAttribute(types.AttributeKeyPointerType, "native"),
		sdk.NewAttribute(types.AttributeKeyPointerAddress, p.Pointer), sdk.NewAttribute(types.AttributeKeyPointee, p.Token),
		sdk.NewAttribute(types.AttributeKeyPointerVersion, fmt.Sprintf("%d", p.Version))))
	if p.Pointer == "" {
		k.DeleteERC20NativePointer(ctx, p.Token, uint16(p.Version))
		return nil
	}
	return k.SetERC20NativePointerWithVersion(ctx, p.Token, common.HexToAddress(p.Pointer), uint16(p.Version))
}

func HandleAddERCCW20PointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddERCCW20PointerProposal) error {
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypePointerRegistered, sdk.NewAttribute(types.AttributeKeyPointerType, "cw20"),
		sdk.NewAttribute(types.AttributeKeyPointerAddress, p.Pointer), sdk.NewAttribute(types.AttributeKeyPointee, p.Pointee),
		sdk.NewAttribute(types.AttributeKeyPointerVersion, fmt.Sprintf("%d", p.Version))))
	if p.Pointer == "" {
		k.DeleteERC20CW20Pointer(ctx, p.Pointee, uint16(p.Version))
		return nil
	}
	return k.SetERC20CW20PointerWithVersion(ctx, p.Pointee, common.HexToAddress(p.Pointer), uint16(p.Version))
}

func HandleAddERCCW721PointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddERCCW721PointerProposal) error {
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypePointerRegistered, sdk.NewAttribute(types.AttributeKeyPointerType, "cw721"),
		sdk.NewAttribute(types.AttributeKeyPointerAddress, p.Pointer), sdk.NewAttribute(types.AttributeKeyPointee, p.Pointee),
		sdk.NewAttribute(types.AttributeKeyPointerVersion, fmt.Sprintf("%d", p.Version))))
	if p.Pointer == "" {
		k.DeleteERC721CW721Pointer(ctx, p.Pointee, uint16(p.Version))
		return nil
	}
	return k.SetERC721CW721PointerWithVersion(ctx, p.Pointee, common.HexToAddress(p.Pointer), uint16(p.Version))
}

func HandleAddERCCW1155PointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddERCCW1155PointerProposal) error {
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypePointerRegistered, sdk.NewAttribute(types.AttributeKeyPointerType, "cw1155"),
		sdk.NewAttribute(types.AttributeKeyPointerAddress, p.Pointer), sdk.NewAttribute(types.AttributeKeyPointee, p.Pointee),
		sdk.NewAttribute(types.AttributeKeyPointerVersion, fmt.Sprintf("%d", p.Version))))
	if p.Pointer == "" {
		k.DeleteERC1155CW1155Pointer(ctx, p.Pointee, uint16(p.Version))
		return nil
	}
	return k.SetERC1155CW1155PointerWithVersion(ctx, p.Pointee, common.HexToAddress(p.Pointer), uint16(p.Version))
}

func HandleAddCWERC20PointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddCWERC20PointerProposal) error {
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypePointerRegistered, sdk.NewAttribute(types.AttributeKeyPointerType, "erc20"),
		sdk.NewAttribute(types.AttributeKeyPointerAddress, p.Pointer), sdk.NewAttribute(types.AttributeKeyPointee, p.Pointee),
		sdk.NewAttribute(types.AttributeKeyPointerVersion, fmt.Sprintf("%d", p.Version))))
	if p.Pointer == "" {
		k.DeleteCW20ERC20Pointer(ctx, common.HexToAddress(p.Pointee), uint16(p.Version))
		return nil
	}
	return k.SetCW20ERC20PointerWithVersion(ctx, common.HexToAddress(p.Pointee), p.Pointer, uint16(p.Version))
}

func HandleAddCWERC721PointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddCWERC721PointerProposal) error {
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypePointerRegistered, sdk.NewAttribute(types.AttributeKeyPointerType, "erc721"),
		sdk.NewAttribute(types.AttributeKeyPointerAddress, p.Pointer), sdk.NewAttribute(types.AttributeKeyPointee, p.Pointee),
		sdk.NewAttribute(types.AttributeKeyPointerVersion, fmt.Sprintf("%d", p.Version))))
	if p.Pointer == "" {
		k.DeleteCW721ERC721Pointer(ctx, common.HexToAddress(p.Pointee), uint16(p.Version))
		return nil
	}
	return k.SetCW721ERC721PointerWithVersion(ctx, common.HexToAddress(p.Pointee), p.Pointer, uint16(p.Version))
}

func HandleAddCWERC1155PointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddCWERC1155PointerProposal) error {
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypePointerRegistered, sdk.NewAttribute(types.AttributeKeyPointerType, "erc1155"),
		sdk.NewAttribute(types.AttributeKeyPointerAddress, p.Pointer), sdk.NewAttribute(types.AttributeKeyPointee, p.Pointee),
		sdk.NewAttribute(types.AttributeKeyPointerVersion, fmt.Sprintf("%d", p.Version))))
	if p.Pointer == "" {
		k.DeleteCW1155ERC1155Pointer(ctx, common.HexToAddress(p.Pointee), uint16(p.Version))
		return nil
	}
	return k.SetCW1155ERC1155PointerWithVersion(ctx, common.HexToAddress(p.Pointee), p.Pointer, uint16(p.Version))
}
