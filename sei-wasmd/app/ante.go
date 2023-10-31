package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	ibcante "github.com/cosmos/ibc-go/v3/modules/core/ante"
	"github.com/cosmos/ibc-go/v3/modules/core/keeper"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmTypes "github.com/CosmWasm/wasmd/x/wasm/types"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
)

// HandlerOptions extend the SDK's AnteHandler options by requiring the IBC
// channel keeper.
type HandlerOptions struct {
	ante.HandlerOptions

	IBCKeeper         *keeper.Keeper
	WasmConfig        *wasmTypes.WasmConfig
	TXCounterStoreKey sdk.StoreKey
}

func NewAnteHandler(options HandlerOptions) (sdk.AnteHandler, sdk.AnteDepGenerator, error) {
	if options.AccountKeeper == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "account keeper is required for AnteHandler")
	}
	if options.BankKeeper == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "bank keeper is required for AnteHandler")
	}
	if options.SignModeHandler == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "sign mode handler is required for ante builder")
	}
	if options.ParamsKeeper == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "params keeper is required for ante builder")
	}
	if options.WasmConfig == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "wasm config is required for ante builder")
	}
	if options.TXCounterStoreKey == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "tx counter key is required for ante builder")
	}

	sigGasConsumer := options.SigGasConsumer
	if sigGasConsumer == nil {
		sigGasConsumer = ante.DefaultSigVerificationGasConsumer
	}

	anteDecorators := []sdk.AnteFullDecorator{
		sdk.DefaultWrappedAnteDecorator(ante.NewDefaultSetUpContextDecorator()),                                           // outermost AnteDecorator. SetUpContext must be called first
		sdk.DefaultWrappedAnteDecorator(wasmkeeper.NewLimitSimulationGasDecorator(options.WasmConfig.SimulationGasLimit)), // after setup context to enforce limits early
		sdk.DefaultWrappedAnteDecorator(wasmkeeper.NewCountTXDecorator(options.TXCounterStoreKey)),
		sdk.DefaultWrappedAnteDecorator(ante.NewRejectExtensionOptionsDecorator()),
		sdk.DefaultWrappedAnteDecorator(ante.NewValidateBasicDecorator()),
		sdk.DefaultWrappedAnteDecorator(ante.NewTxTimeoutHeightDecorator()),
		sdk.DefaultWrappedAnteDecorator(ante.NewValidateMemoDecorator(options.AccountKeeper)),
		ante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
		ante.NewDeductFeeDecorator(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, options.ParamsKeeper.(paramskeeper.Keeper), options.TxFeeChecker),
		sdk.DefaultWrappedAnteDecorator(ante.NewSetPubKeyDecorator(options.AccountKeeper)), // SetPubKeyDecorator must be called before all signature verification decorators
		sdk.DefaultWrappedAnteDecorator(ante.NewValidateSigCountDecorator(options.AccountKeeper)),
		sdk.DefaultWrappedAnteDecorator(ante.NewSigGasConsumeDecorator(options.AccountKeeper, options.SigGasConsumer)),
		sdk.DefaultWrappedAnteDecorator(ante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler)),
		ante.NewIncrementSequenceDecorator(options.AccountKeeper),
		sdk.DefaultWrappedAnteDecorator(ibcante.NewAnteDecorator(options.IBCKeeper)),
	}

	anteHandler, anteDepGenerator := sdk.ChainAnteDecorators(anteDecorators...)

	return anteHandler, anteDepGenerator, nil
}
