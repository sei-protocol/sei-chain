package simapp

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"

	ibcante "github.com/cosmos/ibc-go/v3/modules/core/ante"
	"github.com/cosmos/ibc-go/v3/modules/core/keeper"
)

// HandlerOptions extend the SDK's AnteHandler options by requiring the IBC keeper.
type HandlerOptions struct {
	ante.HandlerOptions

	IBCKeeper *keeper.Keeper
}

// NewAnteHandler creates a new ante handler
func NewAnteHandler(options HandlerOptions) (sdk.AnteHandler, error) {
	if options.AccountKeeper == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "account keeper is required for AnteHandler")
	}
	if options.BankKeeper == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "bank keeper is required for AnteHandler")
	}
	if options.SignModeHandler == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "sign mode handler is required for ante builder")
	}
	if options.ParamsKeeper == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "params keeper is required for AnteHandler")
	}
	if options.TxFeeChecker == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "tx fee checker is required for AnteHandler")
	}

	sigGasConsumer := options.SigGasConsumer
	if sigGasConsumer == nil {
		sigGasConsumer = ante.DefaultSigVerificationGasConsumer
	}

	anteDecorators := []sdk.AnteFullDecorator{
		sdk.DefaultWrappedAnteDecorator(ante.NewDefaultSetUpContextDecorator()),
		sdk.DefaultWrappedAnteDecorator(ante.NewRejectExtensionOptionsDecorator()),
		sdk.DefaultWrappedAnteDecorator(ante.NewValidateBasicDecorator()),
		sdk.DefaultWrappedAnteDecorator(ante.NewTxTimeoutHeightDecorator()),
		sdk.DefaultWrappedAnteDecorator(ante.NewValidateMemoDecorator(options.AccountKeeper)),
		sdk.DefaultWrappedAnteDecorator(ante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper)),
		ante.NewDeductFeeDecorator(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, options.ParamsKeeper.(paramskeeper.Keeper), options.TxFeeChecker),
		// SetPubKeyDecorator must be called before all signature verification decorators
		sdk.DefaultWrappedAnteDecorator(ante.NewSetPubKeyDecorator(options.AccountKeeper)),
		sdk.DefaultWrappedAnteDecorator(ante.NewValidateSigCountDecorator(options.AccountKeeper)),
		sdk.DefaultWrappedAnteDecorator(ante.NewSigGasConsumeDecorator(options.AccountKeeper, sigGasConsumer)),
		sdk.DefaultWrappedAnteDecorator(ante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler)),
		sdk.DefaultWrappedAnteDecorator(ante.NewIncrementSequenceDecorator(options.AccountKeeper)),
		sdk.DefaultWrappedAnteDecorator(ibcante.NewAnteDecorator(options.IBCKeeper)),
	}
	anteHandler, _ := sdk.ChainAnteDecorators(anteDecorators...)
	return anteHandler, nil
}
