package types

import (
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/gogo/protobuf/proto"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

type (
	// Fee defines an interface for an application application-defined concrete
	// transaction type to be able to set and return the transaction fee.
	Fee interface {
		GetGas() uint64
		GetAmount() Coins
	}

	// Signature defines an interface for an application application-defined
	// concrete transaction type to be able to set and return transaction signatures.
	Signature interface {
		GetPubKey() cryptotypes.PubKey
		GetSignature() []byte
	}

	// FeeTx defines the interface to be implemented by Tx to use the FeeDecorators
	FeeTx interface {
		seitypes.Tx
		GetGas() uint64
		GetFee() Coins
		FeePayer() seitypes.AccAddress
		FeeGranter() seitypes.AccAddress
	}

	// Tx must have GetMemo() method to use ValidateMemoDecorator
	TxWithMemo interface {
		seitypes.Tx
		GetMemo() string
	}

	// TxWithTimeoutHeight extends the Tx interface by allowing a transaction to
	// set a height timeout.
	TxWithTimeoutHeight interface {
		seitypes.Tx

		GetTimeoutHeight() uint64
	}
)

// MsgTypeURL returns the TypeURL of a `seitypes.Msg`.
func MsgTypeURL(msg seitypes.Msg) string {
	return "/" + proto.MessageName(msg)
}
