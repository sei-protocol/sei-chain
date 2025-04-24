package utils

import (
	"crypto/ecdsa"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
)

func signHash(data []byte) common.Hash {
	hexData := hexutil.Encode(data)
	msg := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(hexData), hexData)
	return crypto.Keccak256Hash([]byte(msg))
}

// Signs a denom with the provided private key. Returns the signature hex that an ethereum wallet would produce.
func GetSignedDenom(privateKey *ecdsa.PrivateKey, denom string) ([]byte, error) {
	if privateKey == nil || privateKey.D == nil {
		return nil, fmt.Errorf("private key is nil")
	}

	if denom == "" {
		return nil, fmt.Errorf("denom is empty")
	}

	// Hash the prefixed message
	prefixedDenom := fmt.Sprintf("ct:%s", denom)
	hash := crypto.Keccak256Hash([]byte(prefixedDenom))

	// Hex encode the hash
	hexData := hexutil.Encode(hash.Bytes())

	// Append the eth sign bytes to the hash, then hash again.
	msg := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(hexData), hexData)
	signedHash := crypto.Keccak256Hash([]byte(msg))

	// Sign the payload
	signature, err := crypto.Sign(signedHash.Bytes(), privateKey)
	if err != nil {
		return nil, err
	}

	// Add 27 for Ethereum compatibility
	v := signature[64] + 27

	signatureWithV := append(signature[:64], v)

	return signatureWithV, nil
}

func GetElGamalKeyPair(pk ecdsa.PrivateKey, denom string) (*elgamal.KeyPair, error) {
	denomKey, err := GetSignedDenom(&pk, denom)
	if err != nil {
		return nil, err
	}
	teg := elgamal.NewTwistedElgamal()
	return teg.KeyGen(denomKey)
}

func GetAESKey(pk ecdsa.PrivateKey, denom string) ([]byte, error) {
	denomKey, err := GetSignedDenom(&pk, denom)
	if err != nil {
		return nil, err
	}

	key, err := encryption.GetAESKey(denomKey)
	if err != nil {
		return nil, err
	}

	return key, nil
}
