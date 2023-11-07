package ethtx

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

func NewAssociateTx(tx *ethtypes.Transaction) (*AssociateTx, error) {
	v, r, s := tx.RawSignatureValues()
	txData := &AssociateTx{
		V: v.Bytes(),
		R: r.Bytes(),
		S: s.Bytes(),
	}
	return txData, nil
}

func (tx *AssociateTx) TxType() byte                       { panic("not implemented") }
func (tx *AssociateTx) Copy() TxData                       { panic("not implemented") }
func (tx *AssociateTx) GetChainID() *big.Int               { panic("not implemented") }
func (tx *AssociateTx) GetAccessList() ethtypes.AccessList { panic("not implemented") }
func (tx *AssociateTx) GetData() []byte                    { panic("not implemented") }
func (tx *AssociateTx) GetNonce() uint64                   { panic("not implemented") }
func (tx *AssociateTx) GetGas() uint64                     { panic("not implemented") }
func (tx *AssociateTx) GetGasPrice() *big.Int              { panic("not implemented") }
func (tx *AssociateTx) GetGasTipCap() *big.Int             { panic("not implemented") }
func (tx *AssociateTx) GetGasFeeCap() *big.Int             { panic("not implemented") }
func (tx *AssociateTx) GetValue() *big.Int                 { panic("not implemented") }
func (tx *AssociateTx) GetTo() *common.Address             { panic("not implemented") }

func (tx *AssociateTx) GetRawSignatureValues() (v, r, s *big.Int) {
	return rawSignatureValues(tx.V, tx.R, tx.S)
}
func (tx *AssociateTx) SetSignatureValues(_, _, _, _ *big.Int) { panic("not implemented") }

func (tx *AssociateTx) AsEthereumData() ethtypes.TxData { panic("not implemented") }
func (tx *AssociateTx) Validate() error                 { panic("not implemented") }

func (tx *AssociateTx) Fee() *big.Int  { panic("not implemented") }
func (tx *AssociateTx) Cost() *big.Int { panic("not implemented") }

func (tx *AssociateTx) EffectiveGasPrice(_ *big.Int) *big.Int { panic("not implemented") }
func (tx *AssociateTx) EffectiveFee(_ *big.Int) *big.Int      { panic("not implemented") }
func (tx *AssociateTx) EffectiveCost(_ *big.Int) *big.Int     { panic("not implemented") }

func (tx *AssociateTx) GetBlobHashes() []common.Hash { panic("not implemented") }
func (tx *AssociateTx) GetBlobFeeCap() *big.Int      { panic("not implemented") }
