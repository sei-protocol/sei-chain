package evm

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
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
	if uint16(p.Version) < native.CurrentVersion {
		return fmt.Errorf("cannot register pointer with version smaller than %d", native.CurrentVersion)
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
	if uint16(p.Version) < cw20.CurrentVersion(ctx) {
		return fmt.Errorf("cannot register pointer with version smaller than %d", cw20.CurrentVersion(ctx))
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
	if uint16(p.Version) < cw721.CurrentVersion {
		return fmt.Errorf("cannot register pointer with version smaller than %d", cw721.CurrentVersion)
	}
	return k.SetERC721CW721PointerWithVersion(ctx, p.Pointee, common.HexToAddress(p.Pointer), uint16(p.Version))
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
	if uint16(p.Version) < erc20.CurrentVersion {
		return fmt.Errorf("cannot register pointer with version smaller than %d", erc20.CurrentVersion)
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
	if uint16(p.Version) < erc721.CurrentVersion {
		return fmt.Errorf("cannot register pointer with version smaller than %d", erc721.CurrentVersion)
	}
	return k.SetCW721ERC721PointerWithVersion(ctx, common.HexToAddress(p.Pointee), p.Pointer, uint16(p.Version))
}
