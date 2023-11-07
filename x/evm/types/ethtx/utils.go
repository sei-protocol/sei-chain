package ethtx

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common/math"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Effective gas price is the smaller of base fee + tip limit vs total fee limit
func EffectiveGasPrice(baseFee, feeCap, tipCap *big.Int) *big.Int {
	return math.BigMin(new(big.Int).Add(tipCap, baseFee), feeCap)
}

// Convert a value with the provided converter and set it using the provided setter
func SetConvertIfPresent[U comparable, V any](orig U, converter func(U) V, setter func(V)) {
	var nilU U
	if orig == nilU {
		return
	}

	setter(converter(orig))
}

// validate a ethtypes.Transaction for sdk.Int overflow
func ValidateEthTx(tx *ethtypes.Transaction) error {
	if !IsValidInt256(tx.Value()) {
		return errors.New("value overflow")
	}
	if !IsValidInt256(tx.GasPrice()) {
		return errors.New("gas price overflow")
	}
	if !IsValidInt256(tx.GasFeeCap()) {
		return errors.New("gas fee cap overflow")
	}
	if !IsValidInt256(tx.GasTipCap()) {
		return errors.New("gas tip cap overflow")
	}
	if !IsValidInt256(tx.BlobGasFeeCap()) {
		return errors.New("blob gas fee cap overflow")
	}
	return nil
}

func DecodeSignature(sig []byte) (r, s, v *big.Int, err error) {
	if len(sig) != crypto.SignatureLength {
		err = fmt.Errorf("wrong size for signature: got %d, want %d", len(sig), crypto.SignatureLength)
		return
	}
	r = new(big.Int).SetBytes(sig[:32])
	s = new(big.Int).SetBytes(sig[32:64])
	v = new(big.Int).SetBytes([]byte{sig[64] + 27})
	return r, s, v, nil
}
