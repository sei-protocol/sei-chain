package ed25519_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
)

func TestSignAndValidateEd25519(t *testing.T) {

	privKey := ed25519.GenPrivKey()
	pubKey := privKey.PubKey()

	msg := crypto.CRandBytes(128)
	sig := privKey.Sign(msg)

	// Test the signature
	assert.NoError(t, pubKey.Verify(msg, sig))

	// Mutate the signature, just one bit.
	// TODO: Replace this with a much better fuzzer, tendermint/ed25519/issues/10
	sig[7] ^= byte(0x01)

	assert.Error(t, pubKey.Verify(msg, sig))
}

func TestBatchSafe(t *testing.T) {
	v := ed25519.NewBatchVerifier()

	for i := 0; i <= 38; i++ {
		priv := ed25519.GenPrivKey()
		pub := priv.PubKey()

		var msg []byte
		if i%2 == 0 {
			msg = []byte("easter")
		} else {
			msg = []byte("egg")
		}

		v.Add(pub, msg, priv.Sign(msg))
	}

	require.NoError(t, v.Verify())
}
