package ed25519

import (
	"fmt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
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

func TestSignWithTag(t *testing.T) {
	var keys []SecretKey
	for i := range byte(3) {
		keys = append(keys, TestSecretKey([]byte{i}))
	}
	t.Logf("keys = %+v", keys)
	msg := []byte("test message")
	tag := utils.OrPanic1(NewTag("testTag"))
	for i := range keys {
		for j := range keys {
			if wantErr, err := i != j, keys[j].Public().VerifyWithTag(tag, msg, keys[i].SignWithTag(tag, msg)); wantErr != (err != nil) {
				t.Errorf("keys[%d].Verify(keys[%d].Sign()) = %v, wantErr = %v", j, i, err, wantErr)
			}
		}
	}
}

func TestDomainSeparation(t *testing.T) {
	msg := []byte("test message")
	tag1 := utils.OrPanic1(NewTag("testTag"))
	tag2 := utils.OrPanic1(NewTag("testTag2"))
	k := TestSecretKey([]byte{34, 33})
	sigs := map[utils.Option[Tag]]Signature{}
	sigs[utils.Some(tag1)] = k.SignWithTag(tag1, msg)
	sigs[utils.Some(tag2)] = k.SignWithTag(tag2, msg)
	sigs[utils.None[Tag]()] = k.Sign(msg)
	for _, tag := range utils.Slice(utils.Some(tag1), utils.Some(tag2), utils.None[Tag]()) {
		for wantTag, sig := range sigs {
			var err error
			if tag, ok := tag.Get(); ok {
				err = k.Public().VerifyWithTag(tag, msg, sig)
			} else {
				err = k.Public().Verify(msg, sig)
			}
			require.Equal(t, err == nil, tag == wantTag, "err = %v", err)
		}
	}
}

func TestBatchVerifier(t *testing.T) {
	v := NewBatchVerifier()
	tag := utils.OrPanic1(NewTag("testTag"))

	for i := range 100 {
		priv := TestSecretKey(fmt.Appendf(nil, "test-%v", i))
		pub := priv.Public()
		var msg []byte
		if i%2 == 0 {
			msg = []byte("easter")
		} else {
			msg = []byte("egg")
		}
		if i%3 == 0 {
			v.AddWithTag(pub, tag, msg, priv.SignWithTag(tag, msg))
		} else {
			v.Add(pub, msg, priv.Sign(msg))
		}
	}
	require.NoError(t, v.Verify())
}
