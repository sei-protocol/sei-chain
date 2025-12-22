package ed25519

import (
	"fmt"
	"github.com/tendermint/tendermint/libs/utils/require"
	"testing"
)

func TestSign(t *testing.T) {
	var keys []SecretKey
	for i := range byte(3) {
		keys = append(keys, TestSecretKey([]byte{i}))
	}
	t.Logf("keys = %+v", keys)
	msg := []byte("test message")
	for i := range keys {
		for j := range keys {
			if wantErr, err := i != j, keys[j].Public().Verify(msg, keys[i].Sign(msg)); wantErr != (err != nil) {
				t.Errorf("keys[%d].Verify(keys[%d].Sign()) = %v, wantErr = %v", j, i, err, wantErr)
			}
		}
	}
}

func TestBatchSafe(t *testing.T) {
	v := NewBatchVerifier()

	for i := 0; i <= 38; i++ {
		priv := TestSecretKey(fmt.Appendf(nil, "test-%v", i))
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
