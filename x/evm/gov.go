package evm

import (
	"errors"
	"fmt"
	"math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func HandleAddERCNativePointerProposalV2(ctx sdk.Context, k *keeper.Keeper, p *types.AddERCNativePointerProposalV2) error {
	decimals := uint8(math.MaxUint8)
	if p.Decimals <= uint32(decimals) {
		// should always be the case given validation
		decimals = uint8(p.Decimals)
	}
	return k.RunWithOneOffEVMInstance(
		ctx, func(e *vm.EVM) error {
			_, _, err := k.UpsertERCNativePointer(ctx, e, math.MaxUint64, p.Token, utils.ERCMetadata{Name: p.Name, Symbol: p.Symbol, Decimals: decimals})
			return err
		}, func(s1, s2 string) {
			logNativeV2Error(ctx, p, s1, s2)
		},
	)
}

func logNativeV2Error(ctx sdk.Context, p *types.AddERCNativePointerProposalV2, step string, err string) {
	id := fmt.Sprintf("Title: %s, Description: %s, Token: %s", p.Title, p.Description, p.Token)
	ctx.Logger().Error(fmt.Sprintf("proposal (%s) encountered error during (%s) due to (%s)", id, step, err))
}

func HandleAddERCNativePointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddERCNativePointerProposal) error {
	return errors.New("proposal type deprecated")
}

func HandleAddERCCW20PointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddERCCW20PointerProposal) error {
	return errors.New("proposal type deprecated")
}

func HandleAddERCCW721PointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddERCCW721PointerProposal) error {
	return errors.New("proposal type deprecated")
}

func HandleAddERCCW1155PointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddERCCW1155PointerProposal) error {
	return errors.New("proposal type deprecated")
}

func HandleAddCWERC20PointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddCWERC20PointerProposal) error {
	return errors.New("proposal type deprecated")
}

func HandleAddCWERC721PointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddCWERC721PointerProposal) error {
	return errors.New("proposal type deprecated")
}

func HandleAddCWERC1155PointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddCWERC1155PointerProposal) error {
	return errors.New("proposal type deprecated")
}
