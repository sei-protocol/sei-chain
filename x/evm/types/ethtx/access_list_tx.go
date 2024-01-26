package ethtx

import (
	"errors"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

func NewAccessListTx(tx *ethtypes.Transaction) (*AccessListTx, error) {
	if err := ValidateEthTx(tx); err != nil {
		return nil, err
	}
	txData := &AccessListTx{
		Nonce:    tx.Nonce(),
		Data:     tx.Data(),
		GasLimit: tx.Gas(),
	}

	v, r, s := tx.RawSignatureValues()

	SetConvertIfPresent(tx.To(), func(to *common.Address) string { return to.Hex() }, txData.SetTo)
	SetConvertIfPresent(tx.Value(), sdk.NewIntFromBigInt, txData.SetAmount)
	SetConvertIfPresent(tx.GasPrice(), sdk.NewIntFromBigInt, txData.SetGasPrice)
	al := tx.AccessList()
	SetConvertIfPresent(&al, NewAccessList, txData.SetAccesses)

	txData.SetSignatureValues(tx.ChainId(), v, r, s)

	return txData, txData.Validate()
}

func (tx *AccessListTx) TxType() uint8 {
	return ethtypes.AccessListTxType
}

func (tx *AccessListTx) Copy() TxData {
	return &AccessListTx{
		ChainID:  tx.ChainID,
		Nonce:    tx.Nonce,
		GasPrice: tx.GasPrice,
		GasLimit: tx.GasLimit,
		To:       tx.To,
		Amount:   tx.Amount,
		Data:     common.CopyBytes(tx.Data),
		Accesses: tx.Accesses,
		V:        common.CopyBytes(tx.V),
		R:        common.CopyBytes(tx.R),
		S:        common.CopyBytes(tx.S),
	}
}

func (tx *AccessListTx) GetChainID() *big.Int {
	if tx.ChainID == nil {
		return nil
	}

	return tx.ChainID.BigInt()
}

func (tx *AccessListTx) GetAccessList() ethtypes.AccessList {
	if tx.Accesses == nil {
		return nil
	}
	return *tx.Accesses.ToEthAccessList()
}

func (tx *AccessListTx) GetData() []byte {
	return common.CopyBytes(tx.Data)
}

func (tx *AccessListTx) GetGas() uint64 {
	return tx.GasLimit
}

func (tx *AccessListTx) GetGasPrice() *big.Int {
	if tx.GasPrice == nil {
		return nil
	}
	return tx.GasPrice.BigInt()
}

func (tx *AccessListTx) GetGasTipCap() *big.Int {
	return tx.GetGasPrice()
}

func (tx *AccessListTx) GetGasFeeCap() *big.Int {
	return tx.GetGasPrice()
}

func (tx *AccessListTx) GetValue() *big.Int {
	if tx.Amount == nil {
		return nil
	}

	return tx.Amount.BigInt()
}

func (tx *AccessListTx) GetNonce() uint64 { return tx.Nonce }

func (tx *AccessListTx) GetTo() *common.Address {
	if tx.To == "" {
		return nil
	}
	to := common.HexToAddress(tx.To)
	return &to
}

func (tx *AccessListTx) AsEthereumData() ethtypes.TxData {
	v, r, s := tx.GetRawSignatureValues()
	return &ethtypes.AccessListTx{
		ChainID:    tx.GetChainID(),
		Nonce:      tx.GetNonce(),
		GasPrice:   tx.GetGasPrice(),
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

func (tx *AccessListTx) GetRawSignatureValues() (v, r, s *big.Int) {
	return rawSignatureValues(tx.V, tx.R, tx.S)
}

func (tx *AccessListTx) SetSignatureValues(chainID, v, r, s *big.Int) {
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

func (tx *AccessListTx) GetBlobFeeCap() *big.Int {
	return nil
}

func (tx *AccessListTx) GetBlobHashes() []common.Hash {
	return nil
}

func (tx AccessListTx) Validate() error {
	gasPrice := tx.GetGasPrice()
	if gasPrice == nil {
		return errors.New("gas price cannot be nil")
	}
	if gasPrice.Sign() == -1 {
		return fmt.Errorf("gas price cannot be negative %s", gasPrice)
	}

	amount := tx.GetValue()
	// Amount can be 0
	if amount != nil && amount.Sign() == -1 {
		return fmt.Errorf("amount cannot be negative %s", amount)
	}

	if !IsValidInt256(tx.Fee()) {
		return errors.New("fee out of bound")
	}

	if tx.To != "" {
		if err := ValidateAddress(tx.To); err != nil {
			return errors.New("invalid to address")
		}
	}

	chainID := tx.GetChainID()

	if chainID == nil {
		return errors.New("chain ID must be present on AccessList txs")
	}

	return nil
}

func (tx AccessListTx) Fee() *big.Int {
	return fee(tx.GetGasPrice(), tx.GetGas())
}

func (tx AccessListTx) Cost() *big.Int {
	return cost(tx.Fee(), tx.GetValue())
}

func (tx AccessListTx) EffectiveGasPrice(_ *big.Int) *big.Int {
	return tx.GetGasPrice()
}

func (tx AccessListTx) EffectiveFee(_ *big.Int) *big.Int {
	return tx.Fee()
}

func (tx AccessListTx) EffectiveCost(_ *big.Int) *big.Int {
	return tx.Cost()
}
