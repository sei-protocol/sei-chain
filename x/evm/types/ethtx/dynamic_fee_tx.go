package ethtx

import (
	"errors"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

func NewDynamicFeeTx(tx *ethtypes.Transaction) (*DynamicFeeTx, error) {
	txData := &DynamicFeeTx{
		Nonce:    tx.Nonce(),
		Data:     tx.Data(),
		GasLimit: tx.Gas(),
	}

	v, r, s := tx.RawSignatureValues()
	SetConvertIfPresent(tx.To(), func(to *common.Address) string { return to.Hex() }, txData.SetTo)
	MustSetConvertIfPresent(tx.Value(), SafeNewIntFromBigInt, txData.SetAmount)
	MustSetConvertIfPresent(tx.GasFeeCap(), SafeNewIntFromBigInt, txData.SetGasFeeCap)
	MustSetConvertIfPresent(tx.GasTipCap(), SafeNewIntFromBigInt, txData.SetGasTipCap)
	al := tx.AccessList()
	SetConvertIfPresent(&al, NewAccessList, txData.SetAccesses)

	txData.SetSignatureValues(tx.ChainId(), v, r, s)
	return txData, nil
}

func (tx *DynamicFeeTx) TxType() uint8 {
	return ethtypes.DynamicFeeTxType
}

func (tx *DynamicFeeTx) Copy() TxData {
	return &DynamicFeeTx{
		ChainID:   tx.ChainID,
		Nonce:     tx.Nonce,
		GasTipCap: tx.GasTipCap,
		GasFeeCap: tx.GasFeeCap,
		GasLimit:  tx.GasLimit,
		To:        tx.To,
		Amount:    tx.Amount,
		Data:      common.CopyBytes(tx.Data),
		Accesses:  tx.Accesses,
		V:         common.CopyBytes(tx.V),
		R:         common.CopyBytes(tx.R),
		S:         common.CopyBytes(tx.S),
	}
}

func (tx *DynamicFeeTx) GetChainID() *big.Int {
	if tx.ChainID == nil {
		return nil
	}

	return tx.ChainID.BigInt()
}

func (tx *DynamicFeeTx) GetAccessList() ethtypes.AccessList {
	if tx.Accesses == nil {
		return nil
	}
	return *tx.Accesses.ToEthAccessList()
}

func (tx *DynamicFeeTx) GetData() []byte {
	return common.CopyBytes(tx.Data)
}

func (tx *DynamicFeeTx) GetGas() uint64 {
	return tx.GasLimit
}

func (tx *DynamicFeeTx) GetGasPrice() *big.Int {
	return tx.GetGasFeeCap()
}

func (tx *DynamicFeeTx) GetGasTipCap() *big.Int {
	if tx.GasTipCap == nil {
		return nil
	}
	return tx.GasTipCap.BigInt()
}

func (tx *DynamicFeeTx) GetGasFeeCap() *big.Int {
	if tx.GasFeeCap == nil {
		return nil
	}
	return tx.GasFeeCap.BigInt()
}

func (tx *DynamicFeeTx) GetValue() *big.Int {
	if tx.Amount == nil {
		return nil
	}

	return tx.Amount.BigInt()
}

func (tx *DynamicFeeTx) GetNonce() uint64 { return tx.Nonce }

func (tx *DynamicFeeTx) GetTo() *common.Address {
	if tx.To == "" {
		return nil
	}
	to := common.HexToAddress(tx.To)
	return &to
}

func (tx *DynamicFeeTx) AsEthereumData() ethtypes.TxData {
	v, r, s := tx.GetRawSignatureValues()
	return &ethtypes.DynamicFeeTx{
		ChainID:    tx.GetChainID(),
		Nonce:      tx.GetNonce(),
		GasTipCap:  tx.GetGasTipCap(),
		GasFeeCap:  tx.GetGasFeeCap(),
		Gas:        tx.GetGas(),
		To:         tx.GetTo(),
		Value:      tx.GetValue(),
		Data:       tx.GetData(),
		AccessList: tx.GetAccessList(),
		V:          v,
		R:          r,
		S:          s,
	}
}

func (tx *DynamicFeeTx) GetRawSignatureValues() (v, r, s *big.Int) {
	return rawSignatureValues(tx.V, tx.R, tx.S)
}

func (tx *DynamicFeeTx) SetSignatureValues(chainID, v, r, s *big.Int) {
	if v != nil {
		tx.V = v.Bytes()
	}
	if r != nil {
		tx.R = r.Bytes()
	}
	if s != nil {
		tx.S = s.Bytes()
	}
	if chainID != nil {
		chainIDInt := sdk.NewIntFromBigInt(chainID)
		tx.ChainID = &chainIDInt
	}
}

func (tx DynamicFeeTx) Validate() error {
	if tx.GasTipCap == nil {
		return errors.New("gas tip cap cannot nil")
	}

	if tx.GasFeeCap == nil {
		return errors.New("gas fee cap cannot nil")
	}

	if tx.GasTipCap.IsNegative() {
		return fmt.Errorf("gas tip cap cannot be negative %s", tx.GasTipCap)
	}

	if tx.GasFeeCap.IsNegative() {
		return fmt.Errorf("gas fee cap cannot be negative %s", tx.GasFeeCap)
	}

	if !IsValidInt256(tx.GetGasTipCap()) {
		return fmt.Errorf("out of bound")
	}

	if !IsValidInt256(tx.GetGasFeeCap()) {
		return fmt.Errorf("out of bound")
	}

	if tx.GasFeeCap.LT(*tx.GasTipCap) {
		return fmt.Errorf("max priority fee per gas higher than max fee per gas (%s > %s)",
			tx.GasTipCap, tx.GasFeeCap,
		)
	}

	if IsValidInt256(tx.Fee()) {
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

func (tx DynamicFeeTx) Fee() *big.Int {
	return fee(tx.GetGasFeeCap(), tx.GasLimit)
}

func (tx DynamicFeeTx) Cost() *big.Int {
	return cost(tx.Fee(), tx.GetValue())
}

func (tx *DynamicFeeTx) EffectiveGasPrice(baseFee *big.Int) *big.Int {
	return EffectiveGasPrice(baseFee, tx.GasFeeCap.BigInt(), tx.GasTipCap.BigInt())
}

func (tx DynamicFeeTx) EffectiveFee(baseFee *big.Int) *big.Int {
	return fee(tx.EffectiveGasPrice(baseFee), tx.GasLimit)
}

func (tx DynamicFeeTx) EffectiveCost(baseFee *big.Int) *big.Int {
	return cost(tx.EffectiveFee(baseFee), tx.GetValue())
}
