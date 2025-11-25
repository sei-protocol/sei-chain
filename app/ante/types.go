package ante

import (
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
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
