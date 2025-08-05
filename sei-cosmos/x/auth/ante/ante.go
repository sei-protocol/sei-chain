package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
)

// HandlerOptions are the options required for constructing a default SDK AnteHandler.
type HandlerOptions struct {
	AccountKeeper   AccountKeeper
	BankKeeper      types.BankKeeper
	FeegrantKeeper  FeegrantKeeper
	ParamsKeeper    ParamsKeeper
	SignModeHandler authsigning.SignModeHandler
	SigGasConsumer  func(meter sdk.GasMeter, sig signing.SignatureV2, params types.Params) error
	TxFeeChecker    TxFeeChecker
}

// NewAnteHandler returns an AnteHandler that checks and increments sequence
// numbers, checks signatures & account numbers, and deducts fees from the first
// signer.
func NewAnteHandler(options HandlerOptions) (sdk.AnteHandler, sdk.AnteDepGenerator, error) {
	if options.AccountKeeper == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "account keeper is required for ante builder")
	}

	if options.BankKeeper == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "bank keeper is required for ante builder")
	}

	if options.ParamsKeeper == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "params keeper is required for ante builder")
	}

	if options.SignModeHandler == nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "sign mode handler is required for ante builder")
	}

	var sigVerifyDecorator sdk.AnteDecorator
	sequentialVerifyDecorator := NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler)
	sigVerifyDecorator = sequentialVerifyDecorator

	anteDecorators := []sdk.AnteFullDecorator{
		sdk.DefaultWrappedAnteDecorator(NewDefaultSetUpContextDecorator()), // outermost AnteDecorator. SetUpContext must be called first
		sdk.DefaultWrappedAnteDecorator(NewRejectExtensionOptionsDecorator()),
		sdk.DefaultWrappedAnteDecorator(NewValidateBasicDecorator()),
		sdk.DefaultWrappedAnteDecorator(NewTxTimeoutHeightDecorator()),
		sdk.DefaultWrappedAnteDecorator(NewValidateMemoDecorator(options.AccountKeeper)),
		NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
		NewDeductFeeDecorator(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, options.ParamsKeeper.(paramskeeper.Keeper), options.TxFeeChecker),
		sdk.DefaultWrappedAnteDecorator(NewSetPubKeyDecorator(options.AccountKeeper)), // SetPubKeyDecorator must be called before all signature verification decorators
		sdk.DefaultWrappedAnteDecorator(NewValidateSigCountDecorator(options.AccountKeeper)),
		sdk.DefaultWrappedAnteDecorator(NewSigGasConsumeDecorator(options.AccountKeeper, options.SigGasConsumer)),
		sdk.DefaultWrappedAnteDecorator(sigVerifyDecorator),
		NewIncrementSequenceDecorator(options.AccountKeeper),
	}
	anteHandler, anteDepGenerator := sdk.ChainAnteDecorators(anteDecorators...)

	return anteHandler, anteDepGenerator, nil
}
