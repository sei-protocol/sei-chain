//go:build !libsecp256k1_sdk
// +build !libsecp256k1_sdk

package secp256k1

import (
	"testing"

	secp256k1 "github.com/btcsuite/btcd/btcec/v2"
	"github.com/stretchr/testify/require"
)

func TestSignatureNonMalleable(t *testing.T) {
	for i := 0; i < 100; i++ {
		msg := []byte("hello world")
		priv := GenPrivKey()
		sigStr, err := priv.Sign(msg)
		require.NoError(t, err)
		require.Equal(t, 64, len(sigStr))

		pub := priv.PubKey()
		require.True(t, pub.VerifySignature(msg, sigStr))

		// Test with different message should fail
		require.False(t, pub.VerifySignature([]byte("different message"), sigStr))
	}
}

func TestSecp256k1LoadPrivkeyAndSerializeIsIdentity(t *testing.T) {
	numberOfTests := 256
	for i := 0; i < numberOfTests; i++ {
		// Construct and test the private key
		privKeyBytes := [32]byte{}
		copy(privKeyBytes[:], GenPrivKey().Bytes())

		// This function creates a private and public key in the underlying libraries format.
		// The private key is basically calling new(big.Int).SetBytes(pk), which removes leading zero bytes
		priv, _ := secp256k1.PrivKeyFromBytes(privKeyBytes[:])
		require.NotNil(t, priv)

		// Make sure the keys are identical after a serialize/deserialize cycle
		serializedBytes := priv.Serialize()
		require.NotNil(t, serializedBytes)
		require.True(t, len(serializedBytes) == 32)

		// Try to parse the serialized key
		priv2, _ := secp256k1.PrivKeyFromBytes(serializedBytes)
		require.NotNil(t, priv2)

		// Make sure keys are the same
		require.Equal(t, priv.Serialize(), priv2.Serialize())
	}
}
