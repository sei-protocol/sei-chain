package types

import (
	"crypto/ecdsa"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Account wraps address and private key.
type Account struct {
	Address common.Address
	PrivKey *ecdsa.PrivateKey
	Nonce   uint64
}

// NewAccount generates new account.
func NewAccount() (*Account, error) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		return nil, err
	}
	return &Account{
		Address: crypto.PubkeyToAddress(privateKey.PublicKey),
		PrivKey: privateKey,
	}, nil
}

// GetAndIncrementNonce increments the nonce.
func (s *Account) GetAndIncrementNonce() uint64 {
	next := atomic.AddUint64(&s.Nonce, 1)
	return next - 1
}

// GenerateAccounts generates random accounts.
func GenerateAccounts(n int) []*Account {
	result := make([]*Account, 0, n)
	for range n {
		newAcc, err := NewAccount()
		if err != nil {
			panic(err)
		}
		result = append(result, newAcc)
	}
	return result
}
