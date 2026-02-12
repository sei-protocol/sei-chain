package ante

import (
	txtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
)

type TxBody interface {
	GetBody() *txtypes.TxBody
}

type TxAuthInfo interface {
	GetAuthInfo() *txtypes.AuthInfo
}

type TxSignaturesV2 interface {
	GetSignaturesV2() ([]signing.SignatureV2, error)
}
