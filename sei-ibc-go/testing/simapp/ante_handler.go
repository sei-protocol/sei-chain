package simapp

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/ante"
	paramskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/keeper"

	ibcante "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/ante"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/keeper"
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

	anteDecorators := []sdk.AnteDecorator{
		ante.NewDefaultSetUpContextDecorator(),
		ante.NewRejectExtensionOptionsDecorator(),
		ante.NewValidateBasicDecorator(),
		ante.NewTxTimeoutHeightDecorator(),
		ante.NewValidateMemoDecorator(options.AccountKeeper),
		ante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
		ante.NewDeductFeeDecorator(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, options.ParamsKeeper.(paramskeeper.Keeper), options.TxFeeChecker),
		// SetPubKeyDecorator must be called before all signature verification decorators
		ante.NewSetPubKeyDecorator(options.AccountKeeper),
		ante.NewValidateSigCountDecorator(options.AccountKeeper),
		ante.NewSigGasConsumeDecorator(options.AccountKeeper, sigGasConsumer),
		ante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler),
		ante.NewIncrementSequenceDecorator(options.AccountKeeper),
		ibcante.NewAnteDecorator(options.IBCKeeper),
	}
	anteHandler := sdk.ChainAnteDecorators(anteDecorators...)
	return anteHandler, nil
}
