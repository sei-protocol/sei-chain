package app

import (
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/utils/tracing"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/ante"
	paramskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/keeper"
	upgradekeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/keeper"
	ibcante "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/ante"
	ibckeeper "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/keeper"
	wasm "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
	wasmkeeper "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	evmante "github.com/sei-protocol/sei-chain/x/evm/ante"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/oracle"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
)

// HandlerOptions extend the SDK's AnteHandler options by requiring the IBC
// channel keeper.
type HandlerOptions struct {
	ante.HandlerOptions

	IBCKeeper         *ibckeeper.Keeper
	WasmConfig        *wasmtypes.WasmConfig
	WasmKeeper        *wasm.Keeper
	OracleKeeper      *oraclekeeper.Keeper
	EVMKeeper         *evmkeeper.Keeper
	UpgradeKeeper     *upgradekeeper.Keeper
	TXCounterStoreKey sdk.StoreKey
	LatestCtxGetter   func() sdk.Context

	TracingInfo *tracing.Info
}

func NewAnteHandler(options HandlerOptions) (sdk.AnteHandler, sdk.AnteHandler, error) {
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
	if options.ParamsKeeper == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "params keeper is required for ante builder")
	}
	if options.TracingInfo == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "tracing info is required for ante builder")
	}
	if options.EVMKeeper == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "evm keeper is required for ante builder")
	}
	if options.LatestCtxGetter == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "latest context getter is required for ante builder")
	}

	sigGasConsumer := options.SigGasConsumer
	if sigGasConsumer == nil {
		sigGasConsumer = ante.DefaultSigVerificationGasConsumer
	}

	sequentialVerifyDecorator := ante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler)

	anteDecorators := []sdk.AnteDecorator{
		ante.NewSetUpContextDecorator(antedecorators.GetGasMeterSetter(options.ParamsKeeper.(paramskeeper.Keeper))), // outermost AnteDecorator. SetUpContext must be called first
		antedecorators.NewGaslessDecorator([]sdk.AnteDecorator{ante.NewDeductFeeDecorator(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, options.ParamsKeeper.(paramskeeper.Keeper), options.TxFeeChecker)}, *options.OracleKeeper, options.EVMKeeper),
		wasmkeeper.NewLimitSimulationGasDecorator(options.WasmConfig.SimulationGasLimit, antedecorators.GetGasMeterSetter(options.ParamsKeeper.(paramskeeper.Keeper))), // after setup context to enforce limits early
		ante.NewRejectExtensionOptionsDecorator(),
		oracle.NewSpammingPreventionDecorator(*options.OracleKeeper),
		oracle.NewOracleVoteAloneDecorator(),
		ante.NewValidateBasicDecorator(),
		ante.NewTxTimeoutHeightDecorator(),
		ante.NewValidateMemoDecorator(options.AccountKeeper),
		ante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
		// PriorityDecorator must be called after DeductFeeDecorator which sets tx priority based on tx fees
		antedecorators.NewPriorityDecorator(),
		// SetPubKeyDecorator must be called before all signature verification decorators
		ante.NewSetPubKeyDecorator(options.AccountKeeper),
		ante.NewValidateSigCountDecorator(options.AccountKeeper),
		ante.NewSigGasConsumeDecorator(options.AccountKeeper, sigGasConsumer),
		sequentialVerifyDecorator,
		ante.NewIncrementSequenceDecorator(options.AccountKeeper),
		evmante.NewEVMAddressDecorator(options.EVMKeeper, options.EVMKeeper.AccountKeeper()),
		antedecorators.NewAuthzNestedMessageDecorator(),
		ibcante.NewAnteDecorator(options.IBCKeeper),
	}

	anteHandler := sdk.ChainAnteDecorators(anteDecorators...)

	evmAnteDecorators := []sdk.AnteDecorator{
		// NOTE: NewEVMNoCosmosFieldsDecorator must come first to prevent writing state to chain without being charged.
		// E.g. EVMPreprocessDecorator may short-circuit all the later ante handlers if AssociateTx and ignore NewEVMNoCosmosFieldsDecorator.
		evmante.NewEVMNoCosmosFieldsDecorator(),
		evmante.NewEVMPreprocessDecorator(options.EVMKeeper, options.EVMKeeper.AccountKeeper()),
		evmante.NewBasicDecorator(options.EVMKeeper),
		evmante.NewEVMFeeCheckDecorator(options.EVMKeeper, options.UpgradeKeeper),
		evmante.NewEVMSigVerifyDecorator(options.EVMKeeper, options.LatestCtxGetter),
		evmante.NewGasDecorator(options.EVMKeeper),
	}
	evmAnteHandler := sdk.ChainAnteDecorators(evmAnteDecorators...)

	router := evmante.NewEVMRouterDecorator(anteHandler, evmAnteHandler)

	tracerAnteDecorators := []sdk.AnteDecorator{
		// NOTE: NewEVMNoCosmosFieldsDecorator must come first to prevent writing state to chain without being charged.
		// E.g. EVMPreprocessDecorator may short-circuit all the later ante handlers if AssociateTx and ignore NewEVMNoCosmosFieldsDecorator.
		evmante.NewEVMNoCosmosFieldsDecorator(),
		evmante.NewEVMPreprocessDecorator(options.EVMKeeper, options.EVMKeeper.AccountKeeper()),
		evmante.NewBasicDecorator(options.EVMKeeper),
		evmante.NewEVMSigVerifyDecorator(options.EVMKeeper, options.LatestCtxGetter),
	}
	tracerAnteHandler := sdk.ChainAnteDecorators(tracerAnteDecorators...)

	return router.AnteHandle, tracerAnteHandler, nil
}
