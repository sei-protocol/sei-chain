package helpers

import (
	"errors"
	"math/big"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

var (
	big2  = big.NewInt(2)
	big8  = big.NewInt(8)
	big27 = big.NewInt(27)
)

// AdjustV adjusts the V value from a raw signature for pubkey recovery.
// For non-legacy transactions, V is bumped by 27.
// For legacy transactions, V is adjusted based on chainID per EIP-155.
// This function is used by both the EVM ante handler and the Giga executor.
func AdjustV(V *big.Int, txType uint8, chainID *big.Int) *big.Int {
	// Non-legacy TX always needs to be bumped by 27
	if txType != ethtypes.LegacyTxType {
		return new(big.Int).Add(V, big27)
	}

	// Legacy TX needs to be adjusted based on chainID
	// V = V - 2*chainID - 8
	adjusted := new(big.Int).Sub(V, new(big.Int).Mul(chainID, big2))
	return adjusted.Sub(adjusted, big8)
}

func GetAddresses(V *big.Int, R *big.Int, S *big.Int, data common.Hash) (common.Address, sdk.AccAddress, cryptotypes.PubKey, error) {
	pubkey, err := RecoverPubkey(data, R, S, V, true)
	if err != nil {
		return common.Address{}, sdk.AccAddress{}, nil, err
	}

	return GetAddressesFromPubkeyBytes(pubkey)
}

func GetAddressesFromPubkeyBytes(pubkey []byte) (common.Address, sdk.AccAddress, cryptotypes.PubKey, error) {
	evmAddr, err := PubkeyToEVMAddress(pubkey)
	if err != nil {
		return common.Address{}, sdk.AccAddress{}, nil, err
	}
	seiPubkey := PubkeyBytesToSeiPubKey(pubkey)
	seiAddr := sdk.AccAddress(seiPubkey.Address())
	return evmAddr, seiAddr, &seiPubkey, nil
}

// first half of go-ethereum/core/types/transaction_signing.go:recoverPlain
func RecoverPubkey(sighash common.Hash, R, S, Vb *big.Int, homestead bool) ([]byte, error) {
	if Vb.BitLen() > 8 {
		return []byte{}, ethtypes.ErrInvalidSig
	}
	V := byte(Vb.Uint64() - 27)
	if !crypto.ValidateSignatureValues(V, R, S, homestead) {
		return []byte{}, ethtypes.ErrInvalidSig
	}
	// encode the signature in uncompressed format
	r, s := R.Bytes(), S.Bytes()
	sig := make([]byte, crypto.SignatureLength)
	copy(sig[32-len(r):32], r)
	copy(sig[64-len(s):64], s)
	sig[64] = V

	// recover the public key from the signature
	return crypto.Ecrecover(sighash[:], sig)
}

// second half of go-ethereum/core/types/transaction_signing.go:recoverPlain
func PubkeyToEVMAddress(pub []byte) (common.Address, error) {
	if len(pub) == 0 || pub[0] != 4 {
		return common.Address{}, errors.New("invalid public key")
	}
	var addr common.Address
	copy(addr[:], crypto.Keccak256(pub[1:])[12:])
	return addr, nil
}

func PubkeyBytesToSeiPubKey(pub []byte) secp256k1.PubKey {
	pubkeyObj, _ := btcec.ParsePubKey(pub)
	return secp256k1.PubKey{Key: pubkeyObj.SerializeCompressed()}
}

// RecoverAddressesFromTx recovers the sender's EVM address, Sei address, and public key
// from a signed PROTECTED Ethereum transaction using the provided signer.
// This is the core recovery function used by both the EVM ante handler and Giga executor.
//
// IMPORTANT: This function calls AdjustV internally, which is only correct for protected
// (EIP-155) transactions. For unprotected legacy transactions (blocktest only), use
// GetAddresses directly with the raw V value.
//
// The caller must provide the appropriate signer for the context. Use evmante.SignerMap[version](chainID)
// where version is determined by evmante.GetVersion(ctx, ethCfg) to ensure consistent behavior
// with the EVM ante handler.
func RecoverAddressesFromTx(ethTx *ethtypes.Transaction, signer ethtypes.Signer, chainID *big.Int) (common.Address, sdk.AccAddress, cryptotypes.PubKey, error) {
	txHash := signer.Hash(ethTx)
	V, R, S := ethTx.RawSignatureValues()
	adjustedV := AdjustV(V, ethTx.Type(), chainID)
	return GetAddresses(adjustedV, R, S, txHash)
}
