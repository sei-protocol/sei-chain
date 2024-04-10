package evm

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func HandleAddERCNativePointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddERCNativePointerProposal) error {
	if p.Pointer == "" {
		k.DeleteERC20NativePointer(ctx, p.Token, uint16(p.Version))
		return nil
	}
	return k.SetERC20NativePointerWithVersion(ctx, p.Token, common.HexToAddress(p.Pointer), uint16(p.Version))
}

func HandleAddERCCW20PointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddERCCW20PointerProposal) error {
	if p.Pointer == "" {
		k.DeleteERC20CW20Pointer(ctx, p.Pointee, uint16(p.Version))
		return nil
	}
	return k.SetERC20CW20PointerWithVersion(ctx, p.Pointee, common.HexToAddress(p.Pointer), uint16(p.Version))
}

func HandleAddERCCW721PointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddERCCW721PointerProposal) error {
	if p.Pointer == "" {
		k.DeleteERC721CW721Pointer(ctx, p.Pointee, uint16(p.Version))
		return nil
	}
	return k.SetERC721CW721PointerWithVersion(ctx, p.Pointee, common.HexToAddress(p.Pointer), uint16(p.Version))
}
