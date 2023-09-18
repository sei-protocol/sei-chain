package ethtx

import (
	"errors"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/utils"
)

func NewBlobTx(tx *ethtypes.Transaction) (*BlobTx, error) {
	txData := &BlobTx{
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

	if tx.GasFeeCap() != nil {
		gasFeeCapInt, err := SafeNewIntFromBigInt(tx.GasFeeCap())
		if err != nil {
			return nil, err
		}
		txData.GasFeeCap = &gasFeeCapInt
	}

	if tx.GasTipCap() != nil {
		gasTipCapInt, err := SafeNewIntFromBigInt(tx.GasTipCap())
		if err != nil {
			return nil, err
		}
		txData.GasTipCap = &gasTipCapInt
	}

	if tx.AccessList() != nil {
		al := tx.AccessList()
		txData.Accesses = NewAccessList(&al)
	}

	if tx.BlobGasFeeCap() != nil {
		blobGasFeeCap, err := SafeNewIntFromBigInt(tx.BlobGasFeeCap())
		if err != nil {
			return nil, err
		}
		txData.BlobFeeCap = &blobGasFeeCap
	}

	if tx.BlobHashes() != nil {
		txData.BlobHashes = [][]byte{}
		for _, blobHash := range tx.BlobHashes() {
			txData.BlobHashes = append(txData.BlobHashes, blobHash[:])
		}
	}

	txData.SetSignatureValues(tx.ChainId(), v, r, s)
	return txData, nil
}

// TxType returns the tx type
func (tx *BlobTx) TxType() uint8 {
	return ethtypes.BlobTxType
}

// Copy returns an instance with the same field values
func (tx *BlobTx) Copy() TxData {
	return &BlobTx{
		ChainID:    tx.ChainID,
		Nonce:      tx.Nonce,
		GasTipCap:  tx.GasTipCap,
		GasFeeCap:  tx.GasFeeCap,
		GasLimit:   tx.GasLimit,
		To:         tx.To,
		Amount:     tx.Amount,
		Data:       common.CopyBytes(tx.Data),
		Accesses:   tx.Accesses,
		BlobFeeCap: tx.BlobFeeCap,
		BlobHashes: tx.BlobHashes,
		V:          common.CopyBytes(tx.V),
		R:          common.CopyBytes(tx.R),
		S:          common.CopyBytes(tx.S),
	}
}

// GetChainID returns the chain id field from the BlobTx
func (tx *BlobTx) GetChainID() *big.Int {
	if tx.ChainID == nil {
		return nil
	}

	return tx.ChainID.BigInt()
}

// GetAccessList returns the AccessList field.
func (tx *BlobTx) GetAccessList() ethtypes.AccessList {
	if tx.Accesses == nil {
		return nil
	}
	return *tx.Accesses.ToEthAccessList()
}

// GetData returns the a copy of the input data bytes.
func (tx *BlobTx) GetData() []byte {
	return common.CopyBytes(tx.Data)
}

// GetGas returns the gas limit.
func (tx *BlobTx) GetGas() uint64 {
	return tx.GasLimit
}

// GetGasPrice returns the gas fee cap field.
func (tx *BlobTx) GetGasPrice() *big.Int {
	return tx.GetGasFeeCap()
}

// GetGasTipCap returns the gas tip cap field.
func (tx *BlobTx) GetGasTipCap() *big.Int {
	if tx.GasTipCap == nil {
		return nil
	}
	return tx.GasTipCap.BigInt()
}

// GetGasFeeCap returns the gas fee cap field.
func (tx *BlobTx) GetGasFeeCap() *big.Int {
	if tx.GasFeeCap == nil {
		return nil
	}
	return tx.GasFeeCap.BigInt()
}

// GetValue returns the tx amount.
func (tx *BlobTx) GetValue() *big.Int {
	if tx.Amount == nil {
		return nil
	}

	return tx.Amount.BigInt()
}

// GetNonce returns the account sequence for the transaction.
func (tx *BlobTx) GetNonce() uint64 { return tx.Nonce }

// GetTo returns the pointer to the recipient address.
func (tx *BlobTx) GetTo() *common.Address {
	if tx.To == "" {
		return nil
	}
	to := common.HexToAddress(tx.To)
	return &to
}

func (tx *BlobTx) GetBlobFeeCap() *big.Int {
	if tx.BlobFeeCap == nil {
		return nil
	}

	return tx.BlobFeeCap.BigInt()
}

func (tx *BlobTx) GetBlobHashes() []common.Hash {
	if tx.BlobHashes == nil {
		return nil
	}

	return utils.Map(tx.BlobHashes, func(hash []byte) common.Hash {
		commonHash := [common.HashLength]byte{}
		copy(commonHash[:], hash)
		return commonHash
	})
}

// AsEthereumData returns an BlobTx transaction tx from the proto-formatted
// TxData defined on the Cosmos EVM.
func (tx *BlobTx) AsEthereumData() ethtypes.TxData {
	v, r, s := tx.GetRawSignatureValues()
	return &ethtypes.BlobTx{
		ChainID:    uint256.MustFromBig(tx.GetChainID()),
		Nonce:      tx.GetNonce(),
		GasTipCap:  uint256.MustFromBig(tx.GetGasTipCap()),
		GasFeeCap:  uint256.MustFromBig(tx.GetGasFeeCap()),
		Gas:        tx.GetGas(),
		To:         *tx.GetTo(),
		Value:      uint256.MustFromBig(tx.GetValue()),
		Data:       tx.GetData(),
		AccessList: tx.GetAccessList(),
		BlobFeeCap: uint256.MustFromBig(tx.GetBlobFeeCap()),
		BlobHashes: tx.GetBlobHashes(),
		V:          uint256.MustFromBig(v),
		R:          uint256.MustFromBig(r),
		S:          uint256.MustFromBig(s),
	}
}

// GetRawSignatureValues returns the V, R, S signature values of the transaction.
// The return values should not be modified by the caller.
func (tx *BlobTx) GetRawSignatureValues() (v, r, s *big.Int) {
	return rawSignatureValues(tx.V, tx.R, tx.S)
}

// SetSignatureValues sets the signature values to the transaction.
func (tx *BlobTx) SetSignatureValues(chainID, v, r, s *big.Int) {
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

// Validate performs a stateless validation of the tx fields.
func (tx BlobTx) Validate() error {
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

// Fee returns gasprice * gaslimit.
func (tx BlobTx) Fee() *big.Int {
	return fee(tx.GetGasFeeCap(), tx.GasLimit)
}

// Cost returns amount + gasprice * gaslimit.
func (tx BlobTx) Cost() *big.Int {
	return cost(tx.Fee(), tx.GetValue())
}

// EffectiveGasPrice returns the effective gas price
func (tx *BlobTx) EffectiveGasPrice(baseFee *big.Int) *big.Int {
	return EffectiveGasPrice(baseFee, tx.GasFeeCap.BigInt(), tx.GasTipCap.BigInt())
}

// EffectiveFee returns effective_gasprice * gaslimit.
func (tx BlobTx) EffectiveFee(baseFee *big.Int) *big.Int {
	return fee(tx.EffectiveGasPrice(baseFee), tx.GasLimit)
}

// EffectiveCost returns amount + effective_gasprice * gaslimit.
func (tx BlobTx) EffectiveCost(baseFee *big.Int) *big.Int {
	return cost(tx.EffectiveFee(baseFee), tx.GetValue())
}
