package signing

import (
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// SigVerifiableTx defines a transaction interface for all signature verification
// handlers.
type SigVerifiableTx interface {
	seitypes.Tx
	GetSigners() []seitypes.AccAddress
	GetPubKeys() ([]cryptotypes.PubKey, error) // If signer already has pubkey in context, this list will have nil in its place
	GetSignaturesV2() ([]signing.SignatureV2, error)
}

// Tx defines a transaction interface that supports all standard message, signature
// fee, memo, and auxiliary interfaces.
type Tx interface {
	SigVerifiableTx

	types.TxWithMemo
	types.FeeTx
	types.TxWithTimeoutHeight
}
