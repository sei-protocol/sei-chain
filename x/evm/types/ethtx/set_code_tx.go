package ethtx

import (
	"errors"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

func NewSetCodeTx(tx *ethtypes.Transaction) (*SetCodeTx, error) {
	if err := ValidateEthTx(tx); err != nil {
		return nil, err
	}
	txData := &SetCodeTx{
		Nonce:    tx.Nonce(),
		Data:     tx.Data(),
		GasLimit: tx.Gas(),
	}
	v, r, s := tx.RawSignatureValues()
	SetConvertIfPresent(tx.To(), func(to *common.Address) string { return to.Hex() }, txData.SetTo)
	SetConvertIfPresent(tx.Value(), sdk.NewIntFromBigInt, txData.SetAmount)
	SetConvertIfPresent(tx.GasFeeCap(), sdk.NewIntFromBigInt, txData.SetGasFeeCap)
	SetConvertIfPresent(tx.GasTipCap(), sdk.NewIntFromBigInt, txData.SetGasTipCap)
	al := tx.AccessList()
	SetConvertIfPresent(&al, NewAccessList, txData.SetAccesses)
	authList := tx.SetCodeAuthorizations()
	SetConvertIfPresent(&authList, NewAuthList, txData.SetAuthList)

	txData.SetSignatureValues(tx.ChainId(), v, r, s)
	return txData, txData.Validate()
}

func (tx *SetCodeTx) TxType() uint8 {
	return ethtypes.SetCodeTxType
}

func (tx *SetCodeTx) Copy() TxData {
	return &SetCodeTx{
		ChainID:   tx.ChainID,
		Nonce:     tx.Nonce,
		GasTipCap: tx.GasTipCap,
		GasFeeCap: tx.GasFeeCap,
		GasLimit:  tx.GasLimit,
		To:        tx.To,
		Amount:    tx.Amount,
		Data:      common.CopyBytes(tx.Data),
		Accesses:  tx.Accesses,
		AuthList:  tx.AuthList,
		V:         common.CopyBytes(tx.V),
		R:         common.CopyBytes(tx.R),
		S:         common.CopyBytes(tx.S),
	}
}

func (tx *SetCodeTx) GetChainID() *big.Int {
	if tx.ChainID == nil {
		return nil
	}

	return tx.ChainID.BigInt()
}

func (tx *SetCodeTx) GetAccessList() ethtypes.AccessList {
	if tx.Accesses == nil {
		return nil
	}
	return *tx.Accesses.ToEthAccessList()
}

func (tx *SetCodeTx) GetAuthList() []ethtypes.SetCodeAuthorization {
	if tx.AuthList == nil {
		return nil
	}
	return *tx.AuthList.ToEthAuthList()
}

func (tx *SetCodeTx) GetData() []byte {
	return common.CopyBytes(tx.Data)
}

func (tx *SetCodeTx) GetGas() uint64 {
	return tx.GasLimit
}

func (tx *SetCodeTx) GetGasPrice() *big.Int {
	return tx.GetGasFeeCap()
}

func (tx *SetCodeTx) GetGasTipCap() *big.Int {
	if tx.GasTipCap == nil {
		return nil
	}
	return tx.GasTipCap.BigInt()
}

func (tx *SetCodeTx) GetGasFeeCap() *big.Int {
	if tx.GasFeeCap == nil {
		return nil
	}
	return tx.GasFeeCap.BigInt()
}

func (tx *SetCodeTx) GetValue() *big.Int {
	if tx.Amount == nil {
		return nil
	}

	return tx.Amount.BigInt()
}

func (tx *SetCodeTx) GetNonce() uint64 { return tx.Nonce }

func (tx *SetCodeTx) GetTo() *common.Address {
	if tx.To == "" {
		return nil
	}
	to := common.HexToAddress(tx.To)
	return &to
}

func (tx *SetCodeTx) AsEthereumData() ethtypes.TxData {
	v, r, s := tx.GetRawSignatureValues()
	return &ethtypes.SetCodeTx{
		ChainID:    bigToUint256(tx.GetChainID()),
		Nonce:      tx.GetNonce(),
		GasTipCap:  bigToUint256(tx.GetGasTipCap()),
		GasFeeCap:  bigToUint256(tx.GetGasFeeCap()),
		Gas:        tx.GetGas(),
		To:         *tx.GetTo(),
		Value:      bigToUint256(tx.GetValue()),
		Data:       tx.GetData(),
		AccessList: tx.GetAccessList(),
		AuthList:   tx.GetAuthList(),
		V:          bigToUint256(v),
		R:          bigToUint256(r),
		S:          bigToUint256(s),
	}
}

func bigToUint256(b *big.Int) *uint256.Int {
	if b == nil {
		return nil
	}
	u := new(uint256.Int)
	u.SetFromBig(b)
	return u
}

func (tx *SetCodeTx) GetRawSignatureValues() (v, r, s *big.Int) {
	return rawSignatureValues(tx.V, tx.R, tx.S)
}

func (tx *SetCodeTx) SetSignatureValues(chainID, v, r, s *big.Int) {
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

func (tx *SetCodeTx) GetBlobFeeCap() *big.Int {
	return nil
}

func (tx *SetCodeTx) GetBlobHashes() []common.Hash {
	return nil
}

func (tx SetCodeTx) Validate() error {
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

	if tx.GasFeeCap.LT(*tx.GasTipCap) {
		return fmt.Errorf("max priority fee per gas higher than max fee per gas (%s > %s)",
			tx.GasTipCap, tx.GasFeeCap,
		)
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

func (tx SetCodeTx) Fee() *big.Int {
	return fee(tx.GetGasFeeCap(), tx.GasLimit)
}

func (tx SetCodeTx) Cost() *big.Int {
	return cost(tx.Fee(), tx.GetValue())
}

func (tx *SetCodeTx) EffectiveGasPrice(baseFee *big.Int) *big.Int {
	return EffectiveGasPrice(baseFee, tx.GasFeeCap.BigInt(), tx.GasTipCap.BigInt())
}

func (tx SetCodeTx) EffectiveFee(baseFee *big.Int) *big.Int {
	return fee(tx.EffectiveGasPrice(baseFee), tx.GasLimit)
}

func (tx SetCodeTx) EffectiveCost(baseFee *big.Int) *big.Int {
	return cost(tx.EffectiveFee(baseFee), tx.GetValue())
}
