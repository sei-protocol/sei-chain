package ethtx

import (
	"errors"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/utils"
)

func NewLegacyTx(tx *ethtypes.Transaction) (*LegacyTx, error) {
	if err := ValidateEthTx(tx); err != nil {
		return nil, err
	}
	txData := &LegacyTx{
		Nonce:    tx.Nonce(),
		Data:     tx.Data(),
		GasLimit: tx.Gas(),
	}

	v, r, s := tx.RawSignatureValues()
	SetConvertIfPresent(tx.To(), func(to *common.Address) string { return to.Hex() }, txData.SetTo)
	SetConvertIfPresent(tx.Value(), sdk.NewIntFromBigInt, txData.SetAmount)
	SetConvertIfPresent(tx.GasPrice(), sdk.NewIntFromBigInt, txData.SetGasPrice)

	txData.SetSignatureValues(tx.ChainId(), v, r, s)
	return txData, txData.Validate()
}

func (tx *LegacyTx) TxType() uint8 {
	return ethtypes.LegacyTxType
}

func (tx *LegacyTx) Copy() TxData {
	return &LegacyTx{
		Nonce:    tx.Nonce,
		GasPrice: tx.GasPrice,
		GasLimit: tx.GasLimit,
		To:       tx.To,
		Amount:   tx.Amount,
		Data:     common.CopyBytes(tx.Data),
		V:        common.CopyBytes(tx.V),
		R:        common.CopyBytes(tx.R),
		S:        common.CopyBytes(tx.S),
	}
}

// copied from go-etherem/core/types:deriveChainId
func (tx *LegacyTx) GetChainID() *big.Int {
	v, _, _ := tx.GetRawSignatureValues()
	if v.BitLen() <= 64 {
		v := v.Uint64()
		if v == 27 || v == 28 {
			return new(big.Int)
		}
		return new(big.Int).SetUint64((v - 35) / 2)
	}
	v = new(big.Int).Sub(v, utils.Big35)
	return v.Div(v, utils.Big2)
}

func (tx *LegacyTx) GetAccessList() ethtypes.AccessList {
	return nil
}

func (tx *LegacyTx) GetData() []byte {
	return common.CopyBytes(tx.Data)
}

func (tx *LegacyTx) GetGas() uint64 {
	return tx.GasLimit
}

func (tx *LegacyTx) GetGasPrice() *big.Int {
	if tx.GasPrice == nil {
		return nil
	}
	return tx.GasPrice.BigInt()
}

func (tx *LegacyTx) GetGasTipCap() *big.Int {
	return tx.GetGasPrice()
}

func (tx *LegacyTx) GetGasFeeCap() *big.Int {
	return tx.GetGasPrice()
}

func (tx *LegacyTx) GetValue() *big.Int {
	if tx.Amount == nil {
		return nil
	}
	return tx.Amount.BigInt()
}

func (tx *LegacyTx) GetNonce() uint64 { return tx.Nonce }

func (tx *LegacyTx) GetTo() *common.Address {
	if tx.To == "" {
		return nil
	}
	to := common.HexToAddress(tx.To)
	return &to
}

func (tx *LegacyTx) AsEthereumData() ethtypes.TxData {
	v, r, s := tx.GetRawSignatureValues()
	return &ethtypes.LegacyTx{
		Nonce:    tx.GetNonce(),
		GasPrice: tx.GetGasPrice(),
		Gas:      tx.GetGas(),
		To:       tx.GetTo(),
		Value:    tx.GetValue(),
		Data:     tx.GetData(),
		V:        v,
		R:        r,
		S:        s,
	}
}

func (tx *LegacyTx) GetRawSignatureValues() (v, r, s *big.Int) {
	return rawSignatureValues(tx.V, tx.R, tx.S)
}

func (tx *LegacyTx) SetSignatureValues(_, v, r, s *big.Int) {
	if v != nil {
		tx.V = v.Bytes()
	}
	if r != nil {
		tx.R = r.Bytes()
	}
	if s != nil {
		tx.S = s.Bytes()
	}
}

func (tx *LegacyTx) GetBlobFeeCap() *big.Int {
	return nil
}

func (tx *LegacyTx) GetBlobHashes() []common.Hash {
	return nil
}

func (tx *LegacyTx) Validate() error {
	gasPrice := tx.GetGasPrice()
	if gasPrice == nil {
		return errors.New("gas price cannot be nil")
	}

	if gasPrice.Sign() == -1 {
		return fmt.Errorf("gas price cannot be negative %s", gasPrice)
	}
	if !IsValidInt256(tx.Fee()) {
		return errors.New("fee out of bound")
	}

	amount := tx.GetValue()
	// Amount can be 0
	if amount != nil && amount.Sign() == -1 {
		return fmt.Errorf("amount cannot be negative %s", amount)
	}

	if tx.To != "" {
		if err := ValidateAddress(tx.To); err != nil {
			return errors.New("invalid to address")
		}
	}

	chainID := tx.GetChainID()

	if chainID == nil {
		return errors.New(
			"chain ID must be present on AccessList txs",
		)
	}

	return nil
}

func (tx LegacyTx) Fee() *big.Int {
	return fee(tx.GetGasPrice(), tx.GetGas())
}

func (tx LegacyTx) Cost() *big.Int {
	return cost(tx.Fee(), tx.GetValue())
}

func (tx LegacyTx) EffectiveGasPrice(_ *big.Int) *big.Int {
	return tx.GetGasPrice()
}

func (tx LegacyTx) EffectiveFee(_ *big.Int) *big.Int {
	return tx.Fee()
}

func (tx LegacyTx) EffectiveCost(_ *big.Int) *big.Int {
	return tx.Cost()
}
