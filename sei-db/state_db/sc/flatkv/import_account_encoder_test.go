package flatkv

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// translateAccountRows runs the buffered Translate+Finalize path for a single
// account's fields and returns the physical rows it produces (0 if the account
// is dropped as a no-op, 1 otherwise). It is the reference encoding that
// EncodeImportAccount must match byte-for-byte.
func translateAccountRows(t *testing.T, addr, nonceVal, codeHashVal []byte, height int64) []PhysicalKVPair {
	t.Helper()
	tr := NewImportTranslator(height)
	var pairs []*proto.KVPair
	if nonceVal != nil {
		pairs = append(pairs, &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr), Value: nonceVal})
	}
	if codeHashVal != nil {
		pairs = append(pairs, &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, addr), Value: codeHashVal})
	}
	emitted, err := tr.Translate(&proto.NamedChangeSet{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: pairs},
	})
	require.NoError(t, err)
	require.Empty(t, emitted, "account fields are buffered, not emitted by Translate")
	return tr.Finalize()
}

func TestEncodeImportAccount_MatchesTranslateFinalize(t *testing.T) {
	const height = int64(7)
	addr := make([]byte, keys.AddressLen)
	for i := range addr {
		addr[i] = byte(i + 1)
	}

	nonceVal := []byte{0, 0, 0, 0, 0, 0, 0, 42}
	codeHashVal := make([]byte, vtype.CodeHashLen)
	for i := range codeHashVal {
		codeHashVal[i] = byte(0xA0 + i)
	}

	cases := []struct {
		name        string
		nonceVal    []byte
		codeHashVal []byte
	}{
		{"nonce-only", nonceVal, nil},
		{"codehash-only", nil, codeHashVal},
		{"both", nonceVal, codeHashVal},
		// All-zero payload: the buffered path drops it (no row); the encoder
		// must report emit == false so callers drop it too.
		{"zero-nonce-only", make([]byte, vtype.NonceLen), nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantRows := translateAccountRows(t, addr, tc.nonceVal, tc.codeHashVal, height)

			got, emit, err := EncodeImportAccount(addr, tc.nonceVal, tc.codeHashVal, height)
			require.NoError(t, err)

			if len(wantRows) == 0 {
				require.False(t, emit, "buffered path dropped the account; encoder must not emit")
				return
			}
			require.True(t, emit)
			require.Equal(t, wantRows[0].Key, got.Key, "physical key must match the buffered path")
			require.Equal(t, wantRows[0].Value, got.Value, "serialized account value must match the buffered path")
		})
	}
}

func TestEncodeImportAccount_Errors(t *testing.T) {
	addr := make([]byte, keys.AddressLen)
	nonceVal := []byte{0, 0, 0, 0, 0, 0, 0, 1}

	t.Run("no fields", func(t *testing.T) {
		_, _, err := EncodeImportAccount(addr, nil, nil, 1)
		require.Error(t, err)
	})
	t.Run("bad address length", func(t *testing.T) {
		_, _, err := EncodeImportAccount(addr[:10], nonceVal, nil, 1)
		require.Error(t, err)
	})
	t.Run("bad nonce length", func(t *testing.T) {
		_, _, err := EncodeImportAccount(addr, []byte{0x01}, nil, 1)
		require.Error(t, err)
	})
	t.Run("bad codehash length", func(t *testing.T) {
		_, _, err := EncodeImportAccount(addr, nil, []byte{0x01}, 1)
		require.Error(t, err)
	})
}
