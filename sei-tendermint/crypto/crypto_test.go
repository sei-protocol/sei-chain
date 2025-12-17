package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/crypto/ed25519"
	tmjson "github.com/tendermint/tendermint/libs/json"
	"github.com/tendermint/tendermint/internal/jsontypes"
)

type msgNorm struct {
	Pub,Priv,PubPtr,PrivPtr PubKey
}

type msgWithKeys struct {
	Pub PubKey
	Priv PrivKey
	PubPtr *PubKey
	PrivPtr *PrivKey
}

func (m msgWithKeys) Normalize() msgNorm {
	return msgNorm {m.Pub,m.Priv.PubKey(),*m.PubPtr,m.PrivPtr.PubKey()}
}


func TestKeyJSON(t *testing.T) {
	secret := []byte("tm-test-key-json-seed")
	want := ed25519.GenPrivKeyFromSecret(secret)
	const privKeyJSONHash = "f36adf0dc679100837e9819a73ccefdf073b5a2129db8d200a4262bfd47cd883"
	const pubKeyJSONHash  = "0b0b97c108fbd1305b323676bc33dc5c9309fb947d5cd29f88e9dce1457c6362"

	t.Log("Test secret key encoding.")
	privJSON := utils.OrPanic1(tmjson.Marshal(want))
	utils.OrPanic(utils.TestDiff(hexHash(privJSON), privKeyJSONHash))
	privJSON = utils.OrPanic1(jsontypes.Marshal(want))
	utils.OrPanic(utils.TestDiff(hexHash(privJSON), privKeyJSONHash))
	var got PrivKey
	utils.OrPanic(tmjson.Unmarshal(privJSON, &got))
	utils.OrPanic(utils.TestDiff(want.PubKey(),got.PubKey()))	

	t.Log("Test public key encoding.")
	pubJSON := utils.OrPanic1(tmjson.Marshal(want.PubKey()))
	utils.OrPanic(utils.TestDiff(hexHash(pubJSON), pubKeyJSONHash))
	pubJSON = utils.OrPanic1(tmjson.Marshal(want.PubKey()))
	utils.OrPanic(utils.TestDiff(hexHash(pubJSON), pubKeyJSONHash))
	var gotPubKey PubKey
	utils.OrPanic(tmjson.Unmarshal(pubJSON, &gotPubKey))
	utils.OrPanic(utils.TestDiff(want.PubKey(),gotPubKey))
	
	t.Log("Test nested key encoding.")
	// Only tmjson makes sense here, because jsontypes applies only to top level messages.
	msgWant := msgWithKeys{want.PubKey(),want,utils.Alloc(want.PubKey()),&want}
	msgJSON := utils.OrPanic1(tmjson.Marshal(msgWant))
	t.Logf("msgJSON = %v",string(msgJSON))
	utils.OrPanic(utils.TestDiff(hexHash(pubJSON), "0b0b97c108fbd1305b323676bc33dc5c9309fb947d5cd29f88e9dce1457c6362"))
	var msgGot msgWithKeys
	utils.OrPanic(tmjson.Unmarshal(msgJSON,&msgGot))
	utils.OrPanic(utils.TestDiff(msgWant.Normalize(),msgGot.Normalize()))
}

func hexHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
