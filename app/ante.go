package app

import (
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmTypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	ibcante "github.com/cosmos/ibc-go/v3/modules/core/ante"
	ibckeeper "github.com/cosmos/ibc-go/v3/modules/core/keeper"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	"github.com/sei-protocol/sei-chain/app/antedecorators/depdecorators"
	"github.com/sei-protocol/sei-chain/utils/tracing"
	"github.com/sei-protocol/sei-chain/x/dex"
	dexkeeper "github.com/sei-protocol/sei-chain/x/dex/keeper"
	nitrokeeper "github.com/sei-protocol/sei-chain/x/nitro/keeper"
	"github.com/sei-protocol/sei-chain/x/oracle"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
)

// HandlerOptions extend the SDK's AnteHandler options by requiring the IBC
// channel keeper.
type HandlerOptions struct {
	ante.HandlerOptions

	IBCKeeper         *ibckeeper.Keeper
	WasmConfig        *wasmTypes.WasmConfig
	OracleKeeper      *oraclekeeper.Keeper
	DexKeeper         *dexkeeper.Keeper
	NitroKeeper       *nitrokeeper.Keeper
	TXCounterStoreKey sdk.StoreKey

	TracingInfo *tracing.Info
}

func NewAnteHandlerAndDepGenerator(options HandlerOptions) (sdk.AnteHandler, sdk.AnteDepGenerator, error) {
	if options.AccountKeeper == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "account keeper is required for AnteHandler")
	}
	if options.BankKeeper == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "bank keeper is required for AnteHandler")
	}
	if options.SignModeHandler == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "sign mode handler is required for ante builder")
	}
	if options.WasmConfig == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "wasm config is required for ante builder")
	}
	if options.OracleKeeper == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "oracle keeper is required for ante builder")
	}
	if options.NitroKeeper == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "nitro keeper is required for ante builder")
	}
	if options.TracingInfo == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "tracing info is required for ante builder")
	}

	sigGasConsumer := options.SigGasConsumer
	if sigGasConsumer == nil {
		sigGasConsumer = ante.DefaultSigVerificationGasConsumer
	}

	// var sigVerifyDecorator sdk.AnteDecorator
	sequentialVerifyDecorator := ante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler)
	// if options.BatchVerifier == nil {
	// 	sigVerifyDecorator = sequentialVerifyDecorator
	// } else {
	// 	sigVerifyDecorator = ante.NewBatchSigVerificationDecorator(options.BatchVerifier, sequentialVerifyDecorator)
	// }

	anteDecorators := []sdk.AnteFullDecorator{
		sdk.DefaultWrappedAnteDecorator(ante.NewSetUpContextDecorator()), // outermost AnteDecorator. SetUpContext must be called first
		// TODO: have dex antehandler separate, and then call the individual antehandlers FROM the gasless antehandler decorator wrapper
		sdk.DefaultWrappedAnteDecorator(antedecorators.NewGaslessDecorator([]sdk.AnteDecorator{}, *options.OracleKeeper, *options.NitroKeeper)),
		sdk.DefaultWrappedAnteDecorator(wasmkeeper.NewLimitSimulationGasDecorator(options.WasmConfig.SimulationGasLimit)), // after setup context to enforce limits early
		sdk.DefaultWrappedAnteDecorator(ante.NewRejectExtensionOptionsDecorator()),
		sdk.DefaultWrappedAnteDecorator(oracle.NewSpammingPreventionDecorator(*options.OracleKeeper)),
		sdk.DefaultWrappedAnteDecorator(ante.NewValidateBasicDecorator()),
		sdk.DefaultWrappedAnteDecorator(ante.NewTxTimeoutHeightDecorator()),
		sdk.DefaultWrappedAnteDecorator(ante.NewValidateMemoDecorator(options.AccountKeeper)),
		sdk.CustomDepWrappedAnteDecorator(ante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper), depdecorators.SignerDepDecorator{ReadOnly: true}),
		sdk.DefaultWrappedAnteDecorator(ante.NewDeductFeeDecorator(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, options.TxFeeChecker)),
		// PriorityDecorator must be called after DeductFeeDecorator which sets tx priority based on tx fees
		sdk.DefaultWrappedAnteDecorator(antedecorators.NewPriorityDecorator()),
		// SetPubKeyDecorator must be called before all signature verification decorators
		sdk.CustomDepWrappedAnteDecorator(ante.NewSetPubKeyDecorator(options.AccountKeeper), depdecorators.SignerDepDecorator{ReadOnly: false}),
		sdk.DefaultWrappedAnteDecorator(ante.NewValidateSigCountDecorator(options.AccountKeeper)),
		sdk.CustomDepWrappedAnteDecorator(ante.NewSigGasConsumeDecorator(options.AccountKeeper, sigGasConsumer), depdecorators.SignerDepDecorator{ReadOnly: true}),
		sdk.CustomDepWrappedAnteDecorator(sequentialVerifyDecorator, depdecorators.SignerDepDecorator{ReadOnly: true}),
		sdk.CustomDepWrappedAnteDecorator(ante.NewIncrementSequenceDecorator(options.AccountKeeper), depdecorators.SignerDepDecorator{ReadOnly: false}),
		sdk.DefaultWrappedAnteDecorator(ibcante.NewAnteDecorator(options.IBCKeeper)),
		sdk.DefaultWrappedAnteDecorator(dex.NewTickSizeMultipleDecorator(*options.DexKeeper)),
	}

	anteHandler, anteDepGenerator := sdk.ChainAnteDecorators(anteDecorators...)

	return anteHandler, anteDepGenerator, nil
}
