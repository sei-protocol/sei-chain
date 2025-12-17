package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/internal/jsontypes"
)

func TestKeyJSON(t *testing.T) {
	secret := []byte("tm-test-key-json-seed")
	want := ed25519.GenPrivKeyFromSecret(secret)
	const privKeyJSONHash = "f36adf0dc679100837e9819a73ccefdf073b5a2129db8d200a4262bfd47cd883"
	const pubKeyJSONHash  = "0b0b97c108fbd1305b323676bc33dc5c9309fb947d5cd29f88e9dce1457c6362"

	t.Log("Test secret key encoding.")
	privJSON := utils.OrPanic1(jsontypes.Marshal(want))
	utils.OrPanic(utils.TestDiff(hexHash(privJSON), privKeyJSONHash))
	var got PrivKey
	utils.OrPanic(jsontypes.Unmarshal(privJSON, &got))
	utils.OrPanic(utils.TestDiff(want.PubKey(),got.PubKey()))	

	t.Log("Test public key encoding.")
	pubJSON := utils.OrPanic1(jsontypes.Marshal(want.PubKey()))
	utils.OrPanic(utils.TestDiff(hexHash(pubJSON), pubKeyJSONHash))
	var gotPubKey PubKey
	utils.OrPanic(jsontypes.Unmarshal(pubJSON, &gotPubKey))
	utils.OrPanic(utils.TestDiff(want.PubKey(),gotPubKey))
}

func hexHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
