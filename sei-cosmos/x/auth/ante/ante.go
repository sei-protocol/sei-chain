package ante

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	authsigning "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	paramskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/keeper"
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
func NewAnteHandler(options HandlerOptions) (sdk.AnteHandler, error) {
	if options.AccountKeeper == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "account keeper is required for ante builder")
	}

	if options.BankKeeper == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "bank keeper is required for ante builder")
	}

	if options.ParamsKeeper == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "params keeper is required for ante builder")
	}

	if options.SignModeHandler == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "sign mode handler is required for ante builder")
	}

	var sigVerifyDecorator sdk.AnteDecorator
	sequentialVerifyDecorator := NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler)
	sigVerifyDecorator = sequentialVerifyDecorator

	anteDecorators := []sdk.AnteDecorator{
		NewDefaultSetUpContextDecorator(), // outermost AnteDecorator. SetUpContext must be called first
		NewRejectExtensionOptionsDecorator(),
		NewValidateBasicDecorator(),
		NewTxTimeoutHeightDecorator(),
		NewValidateMemoDecorator(options.AccountKeeper),
		NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
		NewDeductFeeDecorator(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, options.ParamsKeeper.(paramskeeper.Keeper), options.TxFeeChecker),
		NewSetPubKeyDecorator(options.AccountKeeper), // SetPubKeyDecorator must be called before all signature verification decorators
		NewValidateSigCountDecorator(options.AccountKeeper),
		NewSigGasConsumeDecorator(options.AccountKeeper, options.SigGasConsumer),
		sigVerifyDecorator,
		NewIncrementSequenceDecorator(options.AccountKeeper),
	}
	anteHandler := sdk.ChainAnteDecorators(anteDecorators...)

	return anteHandler, nil
}
