package utils

import (
	"encoding/binary"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// RLPEncodeTransaction encodes a transaction to RLP format.
func RLPEncodeTransaction(tx *types.Transaction) ([]byte, error) {
	encodedTx, err := rlp.EncodeToBytes(tx)
	if err != nil {
		return nil, err
	}
	return encodedTx, nil
}

// FormatTransaction formats a transaction and recovers signer.
func FormatTransaction(tx *types.Transaction) ([]byte, error) {
	return RLPEncodeTransaction(tx)
	// return FormatTransactionWithSender(tx, nil).
}

// FormatTransactionWithSender formats a transaction with a given sender.
// TODO: this has issues apparently.
func FormatTransactionWithSender(tx *types.Transaction, sender *common.Address) ([]byte, error) {
	res := []byte{tx.Type()}
	if tx.ChainId() == nil {
		res = append(res, 0)
	} else {
		chainIDBz := tx.ChainId().Bytes()
		res = append(res, byte(len(chainIDBz)))
		res = append(res, chainIDBz...)
	}
	signer := types.NewCancunSigner(tx.ChainId())
	if sender == nil {
		s, err := signer.Sender(tx)
		if err != nil {
			return nil, err
		}
		sender = &s
	}
	var to common.Address
	// if contract deploy, will be zero address here.
	if tx.To() != nil {
		to = *tx.To()
	}
	res = append(res, sender[:]...)
	res = append(res, to[:]...)
	valueBz := tx.Value().Bytes()
	res = append(res, byte(len(valueBz)))
	res = append(res, valueBz...)
	gpBz := tx.GasPrice().Bytes()
	res = append(res, byte(len(gpBz)))
	res = append(res, gpBz...)
	gfcBz := tx.GasFeeCap().Bytes()
	res = append(res, byte(len(gfcBz)))
	res = append(res, gfcBz...)
	gtcBz := tx.GasTipCap().Bytes()
	res = append(res, byte(len(gtcBz)))
	res = append(res, gtcBz...)
	gasBz := make([]byte, 8)
	binary.BigEndian.PutUint64(gasBz, tx.Gas())
	res = append(res, gasBz...)
	nonceBz := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBz, tx.Nonce())
	res = append(res, nonceBz...)
	v, r, s := tx.RawSignatureValues()
	vBz := v.Bytes()
	rBz := r.Bytes()
	sBz := s.Bytes()
	res = append(res, byte(len(vBz)))
	res = append(res, vBz...)
	res = append(res, byte(len(rBz)))
	res = append(res, rBz...)
	res = append(res, byte(len(sBz)))
	res = append(res, sBz...)
	hash := signer.Hash(tx)
	res = append(res, hash[:]...)
	if tx.To() == nil {
		res = append(res, byte(1)) // is contract deploy.
	} else {
		res = append(res, byte(0)) // is not deploy.
	}
	res = append(res, byte(0)) // no access list generation for now
	res = append(res, tx.Data()...)

	return res, nil
}
