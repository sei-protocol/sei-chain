package ethtx

import (
	"errors"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/utils"
)

func NewBlobTx(tx *ethtypes.Transaction) (*BlobTx, error) {
	if err := ValidateEthTx(tx); err != nil {
		return nil, err
	}
	txData := &BlobTx{
		Nonce:    tx.Nonce(),
		Data:     tx.Data(),
		GasLimit: tx.Gas(),
	}

	v, r, s := tx.RawSignatureValues()

	SetConvertIfPresent(tx.To(), func(to *common.Address) string { return to.Hex() }, txData.SetTo)
	// internally BlobTx uses uint256 which is guaranteed to not have overflow, so using NewIntFromBigInt directly here
	SetConvertIfPresent(tx.Value(), sdk.NewIntFromBigInt, txData.SetAmount)
	SetConvertIfPresent(tx.GasFeeCap(), sdk.NewIntFromBigInt, txData.SetGasFeeCap)
	SetConvertIfPresent(tx.GasTipCap(), sdk.NewIntFromBigInt, txData.SetGasTipCap)
	al := tx.AccessList()
	SetConvertIfPresent(&al, NewAccessList, txData.SetAccesses)
	SetConvertIfPresent(tx.BlobGasFeeCap(), sdk.NewIntFromBigInt, txData.SetBlobFeeCap)
	bh := tx.BlobHashes()
	SetConvertIfPresent(&bh, func(hs *[]common.Hash) [][]byte { return utils.Map(*hs, func(h common.Hash) []byte { return h[:] }) }, txData.SetBlobHashes)
	SetConvertIfPresent(tx.BlobTxSidecar(), sidecarConverter, txData.SetBlobSidecar)

	txData.SetSignatureValues(tx.ChainId(), v, r, s)
	return txData, txData.Validate()
}

func (tx *BlobTx) TxType() uint8 {
	return ethtypes.BlobTxType
}

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
		Sidecar:    tx.Sidecar,
		V:          common.CopyBytes(tx.V),
		R:          common.CopyBytes(tx.R),
		S:          common.CopyBytes(tx.S),
	}
}

func (tx *BlobTx) GetChainID() *big.Int {
	if tx.ChainID == nil {
		return nil
	}

	return tx.ChainID.BigInt()
}

func (tx *BlobTx) GetAccessList() ethtypes.AccessList {
	if tx.Accesses == nil {
		return nil
	}
	return *tx.Accesses.ToEthAccessList()
}

func (tx *BlobTx) GetData() []byte {
	return common.CopyBytes(tx.Data)
}

func (tx *BlobTx) GetGas() uint64 {
	return tx.GasLimit
}

func (tx *BlobTx) BlobGas() uint64 {
	return params.BlobTxBlobGasPerBlob * uint64(len(tx.BlobHashes))
}

func (tx *BlobTx) GetGasPrice() *big.Int {
	return tx.GetGasFeeCap()
}

func (tx *BlobTx) GetGasTipCap() *big.Int {
	if tx.GasTipCap == nil {
		return nil
	}
	return tx.GasTipCap.BigInt()
}

func (tx *BlobTx) GetGasFeeCap() *big.Int {
	if tx.GasFeeCap == nil {
		return nil
	}
	return tx.GasFeeCap.BigInt()
}

func (tx *BlobTx) GetValue() *big.Int {
	if tx.Amount == nil {
		return nil
	}

	return tx.Amount.BigInt()
}

func (tx *BlobTx) GetNonce() uint64 { return tx.Nonce }

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

func (tx *BlobTx) GetRawSignatureValues() (v, r, s *big.Int) {
	return rawSignatureValues(tx.V, tx.R, tx.S)
}

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

func (tx BlobTx) Fee() *big.Int {
	return new(big.Int).Add(fee(tx.GetGasFeeCap(), tx.GasLimit), tx.blobFee())
}

func (tx BlobTx) blobFee() *big.Int {
	return fee(tx.GetBlobFeeCap(), tx.BlobGas())
}

func (tx BlobTx) Cost() *big.Int {
	return cost(tx.Fee(), tx.GetValue())
}

func (tx *BlobTx) EffectiveGasPrice(baseFee *big.Int) *big.Int {
	return EffectiveGasPrice(baseFee, tx.GasFeeCap.BigInt(), tx.GasTipCap.BigInt())
}

func (tx BlobTx) EffectiveFee(baseFee *big.Int) *big.Int {
	return new(big.Int).Add(fee(tx.EffectiveGasPrice(baseFee), tx.GasLimit), tx.blobFee())
}

func (tx BlobTx) EffectiveCost(baseFee *big.Int) *big.Int {
	return cost(tx.EffectiveFee(baseFee), tx.GetValue())
}

func sidecarConverter(ethSidecar *ethtypes.BlobTxSidecar) *BlobTxSidecar {
	return &BlobTxSidecar{
		Blobs:       utils.Map(ethSidecar.Blobs, func(b kzg4844.Blob) []byte { return b[:] }),
		Commitments: utils.Map(ethSidecar.Commitments, func(c kzg4844.Commitment) []byte { return c[:] }),
		Proofs:      utils.Map(ethSidecar.Proofs, func(p kzg4844.Proof) []byte { return p[:] }),
	}
}
