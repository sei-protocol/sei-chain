package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	signing "github.com/cosmos/cosmos-sdk/types/tx/signing"
)

// EVMNoCosmosFieldsDecorator ensures all Cosmos tx fields are empty for EVM txs.
type EVMNoCosmosFieldsDecorator struct{}

func NewEVMNoCosmosFieldsDecorator() EVMNoCosmosFieldsDecorator {
	return EVMNoCosmosFieldsDecorator{}
}

func (d EVMNoCosmosFieldsDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	serviceTx, ok := tx.(interface {
		GetBody() *txtypes.TxBody
		GetAuthInfo() *txtypes.AuthInfo
		GetSignaturesV2() ([]signing.SignatureV2, error)
	})
	if !ok {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "tx does not implement GetBody/GetAuthInfo")
	}
	body := serviceTx.GetBody()
	authInfo := serviceTx.GetAuthInfo()

	if body.Memo != "" {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "memo must be empty for EVM txs")
	}
	if body.TimeoutHeight != 0 {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "timeout_height must be zero for EVM txs")
	}
	if len(body.ExtensionOptions) > 0 || len(body.NonCriticalExtensionOptions) > 0 {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "extension options must be empty for EVM txs")
	}
	if len(authInfo.SignerInfos) > 0 {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "signer_infos must be empty for EVM txs")
	}
	if authInfo.Fee != nil {
		if len(authInfo.Fee.Amount) > 0 {
			return ctx, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "fee amount must be empty for EVM txs")
		}
		if authInfo.Fee.Payer != "" || authInfo.Fee.Granter != "" {
			return ctx, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "fee payer and granter must be empty for EVM txs")
		}
	}
	sigs, err := serviceTx.GetSignaturesV2()
	if err != nil {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "could not get signatures")
	}
	if len(sigs) > 0 {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "signatures must be empty for EVM txs")
	}

	return next(ctx, tx, simulate)
}
