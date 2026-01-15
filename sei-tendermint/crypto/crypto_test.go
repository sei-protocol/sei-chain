package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/internal/jsontypes"
	"github.com/tendermint/tendermint/libs/utils/require"
)

var privKey = ed25519.TestSecretKey([]byte("tm-test-key-json-seed"))

func hexHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// tests if hash(jsontypes.Marshal(want)) == wantHash
// returns jsontypes.Unmarshal(jsontypes.Marshal(want))
func testTaggedJSON[T jsontypes.Tagged](t *testing.T, want T, wantHash string) T {
	t.Helper()
	raw, err := jsontypes.Marshal(want)
	require.NoError(t, err)
	t.Logf("%T -> %v", want, string(raw))
	require.Equal(t, wantHash, hexHash(raw))
	var got T
	require.NoError(t, jsontypes.Unmarshal(raw, &got))
	return got
}

func TestTaggedJSON(t *testing.T) {
	pubKey := privKey.Public()
	require.Equal(t, pubKey, testTaggedJSON(t, privKey, "f36adf0dc679100837e9819a73ccefdf073b5a2129db8d200a4262bfd47cd883").Public())
	require.Equal(t, pubKey, testTaggedJSON(t, pubKey, "0b0b97c108fbd1305b323676bc33dc5c9309fb947d5cd29f88e9dce1457c6362"))
}

// tests if hash(json.Marshal(want)) == wantHash
// returns json.Unmarshal(json.Marshal(want))
func testJSON[T any](t *testing.T, want T, wantHash string) T {
	t.Helper()
	raw, err := json.Marshal(want)
	require.NoError(t, err)
	t.Logf("%T -> %v", want, string(raw))
	require.Equal(t, wantHash, hexHash(raw))
	var got T
	require.NoError(t, json.Unmarshal(raw, &got))
	return got
}

func TestJSON(t *testing.T) {
	pubKey := privKey.Public()
	sig := privKey.Sign([]byte{1, 2, 3})
	require.Equal(t, pubKey, testJSON(t, privKey, "ecaae500bfb3a28fe1f6108cb7c18743e0242d37c8e41ddd672d8c62563bec1b").Public())
	require.Equal(t, pubKey, testJSON(t, pubKey, "f7874c043989887e8cfa6a3a3c1dd22432f95745481123e29201ebd21bc4d844"))
	require.Equal(t, sig, testJSON(t, sig, "dd48379c6c07eb1e36b188ea0cf35772a697c1a45ad4d47986ca819262743b71"))
}
