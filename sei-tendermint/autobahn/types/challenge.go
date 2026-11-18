package types

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// ChallengeSig is a validator's signature on a P2P connection challenge.
type ChallengeSig struct {
	utils.ReadOnly
	key PublicKey
	sig ed25519.Signature
}

// SignChallenge signs a P2P connection challenge.
func (k SecretKey) SignChallenge(challenge Challenge) ChallengeSig {
	return ChallengeSig{key: k.Public(), sig: k.key.Sign(challenge[:])}
}

// Key returns the validator key that signed the challenge.
func (s ChallengeSig) Key() PublicKey { return s.key }

// Bytes returns the encoded signature.
func (s ChallengeSig) Bytes() []byte { return s.sig.Bytes() }

// Verify verifies the signature on a P2P connection challenge.
func (s ChallengeSig) Verify(challenge Challenge) error {
	return s.key.key.Verify(challenge[:], s.sig)
}

// ChallengeSigFromBytes decodes a challenge signature for a validator key.
func ChallengeSigFromBytes(key PublicKey, b []byte) (ChallengeSig, error) {
	sig, err := ed25519.SignatureFromBytes(b)
	if err != nil {
		return ChallengeSig{}, err
	}
	return ChallengeSig{key: key, sig: sig}, nil
}
