package ethtx

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

func NewLegacyTx(tx *ethtypes.Transaction) (*LegacyTx, error) {
	txData := &LegacyTx{
		Nonce:    tx.Nonce(),
		Data:     tx.Data(),
		GasLimit: tx.Gas(),
	}

	v, r, s := tx.RawSignatureValues()
	if to := tx.To(); to != nil {
		txData.To = to.Hex()
	}

	if tx.Value() != nil {
		amountInt, err := SafeNewIntFromBigInt(tx.Value())
		if err != nil {
			return nil, err
		}
		txData.Amount = &amountInt
	}

	if tx.GasPrice() != nil {
		gasPriceInt, err := SafeNewIntFromBigInt(tx.GasPrice())
		if err != nil {
			return nil, err
		}
		txData.GasPrice = &gasPriceInt
	}

	txData.SetSignatureValues(tx.ChainId(), v, r, s)
	return txData, nil
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

func (tx *LegacyTx) GetChainID() *big.Int {
	v, _, _ := tx.GetRawSignatureValues()
	return DeriveChainID(v)
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

func (tx LegacyTx) Validate() error {
	gasPrice := tx.GetGasPrice()
	if gasPrice == nil {
		return errors.New("gas price cannot be nil")
	}

	if gasPrice.Sign() == -1 {
		return fmt.Errorf("gas price cannot be negative %s", gasPrice)
	}
	if !IsValidInt256(gasPrice) {
		return errors.New("out of bound")
	}
	if !IsValidInt256(tx.Fee()) {
		return errors.New("out of bound")
	}

	amount := tx.GetValue()
	// Amount can be 0
	if amount != nil && amount.Sign() == -1 {
		return fmt.Errorf("amount cannot be negative %s", amount)
	}
	if !IsValidInt256(amount) {
		return errors.New("out of bound")
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

	if !(chainID.Cmp(big.NewInt(9001)) == 0 || chainID.Cmp(big.NewInt(9000)) == 0) {
		return fmt.Errorf(
			"chain ID must be 9000 or 9001 on Evmos, got %s", chainID,
		)
	}

	return nil
}

// Fee returns gasprice * gaslimit.
func (tx LegacyTx) Fee() *big.Int {
	return fee(tx.GetGasPrice(), tx.GetGas())
}

// Cost returns amount + gasprice * gaslimit.
func (tx LegacyTx) Cost() *big.Int {
	return cost(tx.Fee(), tx.GetValue())
}

// EffectiveGasPrice is the same as GasPrice for LegacyTx
func (tx LegacyTx) EffectiveGasPrice(_ *big.Int) *big.Int {
	return tx.GetGasPrice()
}

// EffectiveFee is the same as Fee for LegacyTx
func (tx LegacyTx) EffectiveFee(_ *big.Int) *big.Int {
	return tx.Fee()
}

// EffectiveCost is the same as Cost for LegacyTx
func (tx LegacyTx) EffectiveCost(_ *big.Int) *big.Int {
	return tx.Cost()
}
