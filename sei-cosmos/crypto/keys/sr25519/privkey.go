package sr25519

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/sr25519/internal"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
)

const (
	PrivKeySize = 32
	PrivKeyName = "tendermint/PrivKeySr25519"
)

type PrivKey struct {
	internal.PrivKey
}

// type conversion
func (m *PrivKey) PubKey() cryptotypes.PubKey {
	return &PubKey{Key: m.PrivKey.PubKey()}
}

// type conversion
func (m *PrivKey) Equals(other cryptotypes.LedgerPrivKey) bool {
	sk2, ok := other.(*PrivKey)
	if !ok {
		return false
	}
	return m.PrivKey.Equals(sk2.PrivKey)
}

func (m *PrivKey) ProtoMessage() {}

func (m *PrivKey) Reset() {
	m.PrivKey = internal.PrivKey{}
}

func (m *PrivKey) String() string {
	return string(m.Bytes())
}

func GenPrivKey() *PrivKey {
	return &PrivKey{internal.GenPrivKey()}
}

func GenPrivKeyFromSecret(secret []byte) *PrivKey {
	return &PrivKey{PrivKey: internal.GenPrivKeyFromSecret(secret)}
}
