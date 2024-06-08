package evm

import (
	"errors"
	"fmt"
	"math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func HandleAddERCNativePointerProposalV2(ctx sdk.Context, k *keeper.Keeper, p *types.AddERCNativePointerProposalV2) error {
	decimals := uint8(math.MaxUint8)
	if p.Decimals <= uint32(decimals) {
		// should always be the case given validation
		decimals = uint8(p.Decimals)
	}
	constructorArguments := []interface{}{
		p.Token, p.Name, p.Symbol, decimals,
	}
	packedArgs, err := native.GetParsedABI().Pack("", constructorArguments...)
	if err != nil {
		logNativeV2Error(ctx, p, "pack arguments", err.Error())
		return err
	}
	bin := append(native.GetBin(), packedArgs...)
	stateDB := state.NewDBImpl(ctx, k, false)
	evmModuleAddress := k.GetEVMAddressOrDefault(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName))
	msg := core.Message{
		From:              evmModuleAddress,
		Nonce:             stateDB.GetNonce(evmModuleAddress),
		Value:             utils.Big0,
		GasLimit:          math.MaxUint64,
		GasPrice:          utils.Big0,
		GasFeeCap:         utils.Big0,
		GasTipCap:         utils.Big0,
		Data:              bin,
		SkipAccountChecks: true,
	}
	gp := core.GasPool(math.MaxUint64)
	blockCtx, err := k.GetVMBlockContext(ctx, gp)
	if err != nil {
		logNativeV2Error(ctx, p, "get block context", err.Error())
		return err
	}
	cfg := types.DefaultChainConfig().EthereumConfig(k.ChainID(ctx))
	txCtx := core.NewEVMTxContext(&msg)
	evmInstance := vm.NewEVM(*blockCtx, txCtx, stateDB, cfg, vm.Config{})
	st := core.NewStateTransition(evmInstance, &msg, &gp, true)
	// TODO: retain existing contract address if any
	res, err := st.TransitionDb()
	if err != nil {
		logNativeV2Error(ctx, p, "deploying pointer", err.Error())
		return err
	}
	if res.Err != nil {
		logNativeV2Error(ctx, p, "deploying pointer (VM)", res.Err.Error())
		return res.Err
	} else {
		surplus, err := stateDB.Finalize()
		if err != nil {
			logNativeV2Error(ctx, p, "finalizing", err.Error())
			return err
		}
		if !surplus.IsZero() {
			// not an error worth quiting for but should be logged
			logNativeV2Error(ctx, p, "finalizing (surplus)", surplus.String())
		}
	}
	contractAddr := crypto.CreateAddress(msg.From, msg.Nonce)
	if err := k.SetERC20NativePointer(ctx, p.Token, contractAddr); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypePointerRegistered, sdk.NewAttribute(types.AttributeKeyPointerType, "native"),
		sdk.NewAttribute(types.AttributeKeyPointerAddress, contractAddr.Hex()), sdk.NewAttribute(types.AttributeKeyPointee, p.Token),
		sdk.NewAttribute(types.AttributeKeyPointerVersion, fmt.Sprintf("%d", native.CurrentVersion))))
	return nil
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
