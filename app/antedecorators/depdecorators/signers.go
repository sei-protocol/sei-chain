package depdecorators

import (
	"encoding/hex"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

type SignerDepDecorator struct {
	ReadOnly bool
}

func (d SignerDepDecorator) AnteDeps(txDeps []sdkacltypes.AccessOperation, tx sdk.Tx, next sdk.AnteDepGenerator) (newTxDeps []sdkacltypes.AccessOperation, err error) {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return txDeps, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "invalid tx type")
	}
	var accessType sdkacltypes.AccessType
	if d.ReadOnly {
		accessType = sdkacltypes.AccessType_READ
	} else {
		accessType = sdkacltypes.AccessType_WRITE
	}
	for _, signer := range sigTx.GetSigners() {
		txDeps = append(txDeps, sdkacltypes.AccessOperation{
			AccessType:         accessType,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: hex.EncodeToString(authtypes.AddressStoreKey(signer)),
		})
	}
	return next(txDeps, tx)
}
