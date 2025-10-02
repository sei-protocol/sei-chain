package sr25519_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/sr25519"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/crypto"
)

func TestSignAndValidateEd25519(t *testing.T) {
	privKey := sr25519.GenPrivKey()
	pubKey := privKey.PubKey()

	msg := crypto.CRandBytes(1000)
	sig, err := privKey.Sign(msg)
	require.Nil(t, err)

	// Test the signature
	require.True(t, pubKey.VerifySignature(msg, sig))

	// ----
	// Mutate the signature, just one bit.
	// TODO: Replace this with a much better fuzzer, tendermint/ed25519/issues/10
	sig[7] ^= byte(0x01)
	require.False(t, pubKey.VerifySignature(msg, sig))
}

func TestPubKeyEquals(t *testing.T) {
	sr25519PubKey := sr25519.GenPrivKey().PubKey().(*sr25519.PubKey)

	testCases := []struct {
		msg      string
		pubKey   cryptotypes.PubKey
		other    cryptotypes.PubKey
		expectEq bool
	}{
		{
			"different bytes",
			sr25519PubKey,
			sr25519.GenPrivKey().PubKey(),
			false,
		},
		{
			"equals",
			sr25519PubKey,
			&sr25519.PubKey{
				Key: sr25519PubKey.Key,
			},
			true,
		},
		{
			"different types",
			sr25519PubKey,
			secp256k1.GenPrivKey().PubKey(),
			false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.msg, func(t *testing.T) {
			eq := tc.pubKey.Equals(tc.other)
			require.Equal(t, eq, tc.expectEq)
		})
	}
}

func TestPrivKeyEquals(t *testing.T) {
	sr25519PrivKey := sr25519.GenPrivKey()

	testCases := []struct {
		msg      string
		privKey  cryptotypes.PrivKey
		other    cryptotypes.PrivKey
		expectEq bool
	}{
		{
			"different bytes",
			sr25519PrivKey,
			sr25519.GenPrivKey(),
			false,
		},
		{
			"equals",
			sr25519PrivKey,
			&sr25519.PrivKey{
				PrivKey: sr25519PrivKey.PrivKey,
			},
			true,
		},
		{
			"different types",
			sr25519PrivKey,
			secp256k1.GenPrivKey(),
			false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.msg, func(t *testing.T) {
			eq := tc.privKey.Equals(tc.other)
			require.Equal(t, eq, tc.expectEq)
		})
	}
}
