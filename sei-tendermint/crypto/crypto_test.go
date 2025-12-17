package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/internal/jsontypes"
	tmjson "github.com/tendermint/tendermint/libs/json"
)

func TestKeyJSON(t *testing.T) {
	secret := []byte("tm-test-key-json-seed")
	want := ed25519.GenPrivKeyFromSecret(secret)
	const privKeyJSONHash = "f36adf0dc679100837e9819a73ccefdf073b5a2129db8d200a4262bfd47cd883"
	const pubKeyJSONHash  = "0b0b97c108fbd1305b323676bc33dc5c9309fb947d5cd29f88e9dce1457c6362"
	const sigJSONHash = "dd48379c6c07eb1e36b188ea0cf35772a697c1a45ad4d47986ca819262743b71"

	t.Log("Test secret key encoding.")
	privJSON := utils.OrPanic1(jsontypes.Marshal(want))
	require.Equal(t,hexHash(privJSON), privKeyJSONHash)
	var got PrivKey
	require.NoError(t,jsontypes.Unmarshal(privJSON, &got))
	require.Equal(t,want.PubKey(),got.PubKey())

	t.Log("Test public key encoding.")
	pubJSON := utils.OrPanic1(jsontypes.Marshal(want.PubKey()))
	t.Logf("pubJSON = %v",string(pubJSON))
	require.Equal(t,hexHash(pubJSON), pubKeyJSONHash)
	var gotPubKey PubKey
	require.NoError(t,jsontypes.Unmarshal(pubJSON, &gotPubKey))
	require.Equal(t,want.PubKey(),gotPubKey)

	t.Log("Test signature encoding.")
	wantSig := want.Sign([]byte{1,2,3})
	sigJSON := utils.OrPanic1(tmjson.Marshal(wantSig))
	t.Logf("sigJSON = %v",string(sigJSON))
	require.Equal(t,hexHash(sigJSON), sigJSONHash)
	var gotSig Sig
	require.NoError(t,tmjson.Unmarshal(sigJSON, &gotSig))
	require.Equal(t,wantSig,gotSig)
}

func hexHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
