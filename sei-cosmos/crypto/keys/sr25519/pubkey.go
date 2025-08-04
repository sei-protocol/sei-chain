package sr25519

import (
	"bytes"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/tendermint/tendermint/crypto"
)

const PubKeyName = "tendermint/PubKeySr25519"

func (m *PubKey) Equals(other cryptotypes.PubKey) bool {
	pk2, ok := other.(*PubKey)
	if !ok {
		return false
	}
	return bytes.Equal(m.Key, pk2.Key)
}

func (m *PubKey) Address() crypto.Address {
	return m.Key.Address()
}

func (m PubKey) Bytes() []byte {
	return m.Key.Bytes()
}

func (m PubKey) String() string {
	return m.Key.String()
}

func (m PubKey) Type() string {
	return "sr25519"
}

func (m PubKey) VerifySignature(msg []byte, sigBytes []byte) bool {
	return m.Key.VerifySignature(msg, sigBytes)
}
