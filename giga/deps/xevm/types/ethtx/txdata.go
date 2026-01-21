package ethtx

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/gogo/protobuf/proto"
	"github.com/sei-protocol/sei-chain/utils"
)

var (
	_ TxData = &LegacyTx{}
	_ TxData = &AccessListTx{}
	_ TxData = &DynamicFeeTx{}
	_ TxData = &BlobTx{}
	_ TxData = &AssociateTx{}
	_ TxData = &SetCodeTx{}
)

// Unfortunately `TxData` interface in go-ethereum/core/types defines its functions
// as private, so we have to define our own here.
type TxData interface {
	proto.Message
	TxType() byte
	Copy() TxData
	GetChainID() *big.Int
	GetAccessList() ethtypes.AccessList
	GetData() []byte
	GetNonce() uint64
	GetGas() uint64
	GetGasPrice() *big.Int
	GetGasTipCap() *big.Int
	GetGasFeeCap() *big.Int
	GetValue() *big.Int
	GetTo() *common.Address

	GetRawSignatureValues() (v, r, s *big.Int)
	SetSignatureValues(chainID, v, r, s *big.Int)

	AsEthereumData() ethtypes.TxData
	Validate() error

	Fee() *big.Int
	Cost() *big.Int

	EffectiveGasPrice(baseFee *big.Int) *big.Int
	EffectiveFee(baseFee *big.Int) *big.Int
	EffectiveCost(baseFee *big.Int) *big.Int

	GetBlobHashes() []common.Hash
	GetBlobFeeCap() *big.Int
}

func NewTxDataFromTx(tx *ethtypes.Transaction) (TxData, error) {
	var txData TxData
	var err error
	switch tx.Type() {
	case ethtypes.DynamicFeeTxType:
		txData, err = NewDynamicFeeTx(tx)
	case ethtypes.AccessListTxType:
		txData, err = NewAccessListTx(tx)
	case ethtypes.BlobTxType:
		txData, err = NewBlobTx(tx)
	case ethtypes.SetCodeTxType:
		txData, err = NewSetCodeTx(tx)
	default:
		txData, err = NewLegacyTx(tx)
	}
	if err != nil {
		return nil, err
	}

	return txData, nil
}

func rawSignatureValues(vBz, rBz, sBz []byte) (v, r, s *big.Int) {
	if len(vBz) > 0 {
		v = new(big.Int).SetBytes(vBz)
	} else {
		v = utils.Big0
	}
	if len(rBz) > 0 {
		r = new(big.Int).SetBytes(rBz)
	} else {
		r = utils.Big0
	}
	if len(sBz) > 0 {
		s = new(big.Int).SetBytes(sBz)
	} else {
		s = utils.Big0
	}
	return v, r, s
}

// fee = gas limit * gas price
func fee(gasPrice *big.Int, gas uint64) *big.Int {
	gasLimit := new(big.Int).SetUint64(gas)
	return new(big.Int).Mul(gasPrice, gasLimit)
}

// cost = fee + tokens to send
func cost(fee, value *big.Int) *big.Int {
	if value != nil {
		return new(big.Int).Add(fee, value)
	}
	return fee
}
