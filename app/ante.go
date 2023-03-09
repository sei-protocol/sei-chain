package app

import (
	wasm "github.com/CosmWasm/wasmd/x/wasm"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
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

	IBCKeeper           *ibckeeper.Keeper
	WasmConfig          *wasmtypes.WasmConfig
	WasmKeeper          *wasm.Keeper
	OracleKeeper        *oraclekeeper.Keeper
	DexKeeper           *dexkeeper.Keeper
	NitroKeeper         *nitrokeeper.Keeper
	AccessControlKeeper *aclkeeper.Keeper
	TXCounterStoreKey   sdk.StoreKey

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
	if options.WasmKeeper == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "wasm keeper is required for ante builder")
	}
	if options.OracleKeeper == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "oracle keeper is required for ante builder")
	}
	if options.NitroKeeper == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "nitro keeper is required for ante builder")
	}
	if options.AccessControlKeeper == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "accesscontrol keeper is required for ante builder")
	}
	if options.TracingInfo == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "tracing info is required for ante builder")
	}

	sigGasConsumer := options.SigGasConsumer
	if sigGasConsumer == nil {
		sigGasConsumer = ante.DefaultSigVerificationGasConsumer
	}

	sequentialVerifyDecorator := ante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler)

	anteDecorators := []sdk.AnteFullDecorator{
		sdk.CustomDepWrappedAnteDecorator(ante.NewSetUpContextDecorator(antedecorators.GetGasMeterSetter(*options.AccessControlKeeper)), depdecorators.GasMeterSetterDecorator{}), // outermost AnteDecorator. SetUpContext must be called first
		antedecorators.NewGaslessDecorator([]sdk.AnteFullDecorator{ante.NewDeductFeeDecorator(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, options.TxFeeChecker)}, *options.OracleKeeper, *options.NitroKeeper),
		sdk.DefaultWrappedAnteDecorator(wasmkeeper.NewLimitSimulationGasDecorator(options.WasmConfig.SimulationGasLimit)), // after setup context to enforce limits early
		sdk.DefaultWrappedAnteDecorator(ante.NewRejectExtensionOptionsDecorator()),
		oracle.NewSpammingPreventionDecorator(*options.OracleKeeper),
		oracle.NewOracleVoteAloneDecorator(),
		sdk.DefaultWrappedAnteDecorator(ante.NewValidateBasicDecorator()),
		sdk.DefaultWrappedAnteDecorator(ante.NewTxTimeoutHeightDecorator()),
		sdk.DefaultWrappedAnteDecorator(ante.NewValidateMemoDecorator(options.AccountKeeper)),
		ante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
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
		sdk.DefaultWrappedAnteDecorator(dex.NewCheckDexGasDecorator(*options.DexKeeper)),
		antedecorators.NewACLWasmDependencyDecorator(*options.AccessControlKeeper, *options.WasmKeeper),
	}

	anteHandler, anteDepGenerator := sdk.ChainAnteDecorators(anteDecorators...)

	return anteHandler, anteDepGenerator, nil
}
