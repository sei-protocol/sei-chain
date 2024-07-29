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
	gasMultipler := k.GetPriorityNormalizer(ctx)
	var evmGas uint64 = math.MaxUint64
	if ctx.GasMeter() != nil && ctx.GasMeter().Limit() > 0 {
		cosmosGasRemaining := ctx.GasMeter().Limit() - ctx.GasMeter().GasConsumed()
		evmGas = sdk.NewDecFromInt(sdk.NewIntFromUint64(cosmosGasRemaining)).Quo(gasMultipler).TruncateInt().Uint64()
	}
	meter := ctx.GasMeter()
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	return k.RunWithOneOffEVMInstance(
		ctx, func(e *vm.EVM) error {
			_, remainingGas, err := k.UpsertERCNativePointer(ctx, e, evmGas, p.Token, utils.ERCMetadata{Name: p.Name, Symbol: p.Symbol, Decimals: decimals})
			gasConsumed := sdk.NewDecFromInt(sdk.NewIntFromUint64(evmGas - remainingGas)).Mul(gasMultipler).TruncateInt().BigInt()
			meter.ConsumeGas(gasConsumed.Uint64(), "one-off EVM instance")
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

func HandleAddCWERC20PointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddCWERC20PointerProposal) error {
	return errors.New("proposal type deprecated")
}

func HandleAddCWERC721PointerProposal(ctx sdk.Context, k *keeper.Keeper, p *types.AddCWERC721PointerProposal) error {
	return errors.New("proposal type deprecated")
}
