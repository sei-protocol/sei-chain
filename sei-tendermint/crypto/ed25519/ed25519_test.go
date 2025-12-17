package ed25519

import (
	"testing"
	"fmt"

	"github.com/stretchr/testify/assert"

	"github.com/tendermint/tendermint/libs/utils/require"
)

func TestSignAndValidateEd25519(t *testing.T) {
	privKey := TestSecretKey([]byte("test"))
	pubKey := privKey.Public()
	msg := []byte("message")
	sig := privKey.Sign(msg)

	// Test the signature
	assert.NoError(t, pubKey.Verify(msg, sig))

	// Mutate the signature, just one bit.
	// TODO: Replace this with a much better fuzzer, tendermint/ed25519/issues/10
	sig.sig[7] ^= byte(0x01)

	assert.Error(t, pubKey.Verify(msg, sig))
}

func TestBatchSafe(t *testing.T) {
	v := NewBatchVerifier()

	for i := 0; i <= 38; i++ {
		priv := TestSecretKey(fmt.Appendf(nil,"test-%v",i))
		pub := priv.Public()

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
