package internal

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

var _ types.Signer = (*Signer)(nil)

type Signer struct {
	From common.Address
}

func (sig *Signer) Sender(_ *types.Transaction) (common.Address, error) {
	return sig.From, nil
}

func (sig *Signer) SignatureValues(_ *types.Transaction, _ []byte) (r, s, v *big.Int, err error) {
	panic("signer.SignatureValues not implemented")
}

func (sig *Signer) ChainID() *big.Int {
	panic("signer.ChainID not implemented")
}

func (sig *Signer) Hash(_ *types.Transaction) common.Hash {
	panic("signer.Hash not implemented")
}

func (sig *Signer) Equal(_ types.Signer) bool {
	panic("signer.Equal not implemented")
}
