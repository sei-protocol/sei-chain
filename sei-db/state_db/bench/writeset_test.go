package bench

import (
	"encoding/hex"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

// prestateFixture is a real debug_traceCall prestateTracer diffMode response
// captured from pacific-1 for bytecode 0x602a60005500
// (PUSH1 0x2a; PUSH1 0; SSTORE; STOP) with a code state override.
const prestateFixture = `{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "post": {
      "0x0000000000000000000000000000000000000001": {"nonce": 1},
      "0x1000000000000000000000000000000000000001": {
        "storage": {
          "0x0000000000000000000000000000000000000000000000000000000000000000":
            "0x000000000000000000000000000000000000000000000000000000000000002a"
        }
      }
    },
    "pre": {
      "0x0000000000000000000000000000000000000001": {"balance": "0x27147114878000"},
      "0x1000000000000000000000000000000000000001": {"balance": "0x0", "code": "0x602a60005500"}
    }
  }
}`

func TestConvertPrestateDiff(t *testing.T) {
	converted, err := ConvertPrestateDiff([]byte(prestateFixture))
	require.NoError(t, err)
	ws := converted.WriteSet
	require.Len(t, ws.Blocks, 1)

	byKind := map[string]int{}
	for _, w := range ws.Blocks[0].Writes {
		byKind[w.Kind]++
	}
	require.Equal(t, 1, byKind[WriteKindStorage], "one SSTORE slot")
	require.Equal(t, 1, byKind[WriteKindNonce], "sender nonce bump")

	changesets, err := ws.BlockChangesets(0)
	require.NoError(t, err)
	require.Len(t, changesets, 1)
	require.Equal(t, "evm", changesets[0].Name)

	var sawStorageKey bool
	for _, pair := range changesets[0].Changeset.Pairs {
		if pair.Key[0] == 0x03 {
			sawStorageKey = true
			require.Len(t, pair.Key, 53, "storage key is 0x03||addr||slot")
			require.Len(t, pair.Value, 32, "storage value padded to 32 bytes")
			require.Equal(t, byte(0x2a), pair.Value[31])
		}
	}
	require.True(t, sawStorageKey)
}

func TestConvertPrestateDiffEmitsDeletes(t *testing.T) {
	trace := `{
      "pre":  {"0x1000000000000000000000000000000000000001": {"storage": {"0x01": "0x2a"}}},
      "post": {"0x1000000000000000000000000000000000000001": {"nonce": 1}}
    }`
	converted, err := ConvertPrestateDiff([]byte(trace))
	require.NoError(t, err)

	var deletes int
	for _, w := range converted.WriteSet.Blocks[0].Writes {
		if w.Delete {
			deletes++
			require.Equal(t, WriteKindStorage, w.Kind)
			require.Len(t, w.Slot, 64, "slot padded to 32 bytes")
		}
	}
	require.Equal(t, 1, deletes, "slot zeroed in post emits a delete")
}

func TestConvertPrestateDiffNoSpuriousDeleteOnEncodingMismatch(t *testing.T) {
	// The same slot is unpadded in pre but padded in post. The delete pass must
	// normalize both before comparing, or it emits a delete that clobbers the
	// write (last-write-wins), losing the updated value.
	trace := `{
      "pre":  {"0x1000000000000000000000000000000000000001": {"storage": {"0x01": "0x2a"}}},
      "post": {"0x1000000000000000000000000000000000000001": {"storage": {
        "0x0000000000000000000000000000000000000000000000000000000000000001": "0x2b"}}}
    }`
	converted, err := ConvertPrestateDiff([]byte(trace))
	require.NoError(t, err)

	for _, w := range converted.WriteSet.Blocks[0].Writes {
		require.False(t, w.Delete, "same slot written in post must not also be deleted")
	}

	changesets, err := converted.WriteSet.BlockChangesets(0)
	require.NoError(t, err)
	var storageWrites int
	for _, pair := range changesets[0].Changeset.Pairs {
		if pair.Key[0] == 0x03 {
			storageWrites++
			require.False(t, pair.Delete)
			require.Equal(t, byte(0x2b), pair.Value[31], "updated value survives")
		}
	}
	require.Equal(t, 1, storageWrites)
}

func TestConvertPrestateDiffCodeDeployment(t *testing.T) {
	trace := `{
      "pre":  {"0x2000000000000000000000000000000000000002": {}},
      "post": {"0x2000000000000000000000000000000000000002": {"code": "0x602a60005500", "nonce": 1}}
    }`
	converted, err := ConvertPrestateDiff([]byte(trace))
	require.NoError(t, err)

	byKind := map[string]WriteSetEntry{}
	for _, w := range converted.WriteSet.Blocks[0].Writes {
		byKind[w.Kind] = w
	}
	require.Contains(t, byKind, WriteKindCode)
	require.Contains(t, byKind, WriteKindCodeHash)
	require.Contains(t, byKind, WriteKindRaw, "codesize write")

	codeHash, err := hex.DecodeString(byKind[WriteKindCodeHash].Value)
	require.NoError(t, err)
	require.Len(t, codeHash, 32)

	rawKey, err := hex.DecodeString(byKind[WriteKindRaw].Key)
	require.NoError(t, err)
	require.Equal(t, byte(0x09), rawKey[0], "codesize key prefix")
	require.Len(t, rawKey, 21)
}

func TestConvertPrestateDiffRequiresDiffMode(t *testing.T) {
	_, err := ConvertPrestateDiff([]byte(`{"pre": {}}`))
	require.ErrorContains(t, err, "diffMode")
}

func TestReplayWriteSetOnBothBackends(t *testing.T) {
	converted, err := ConvertPrestateDiff([]byte(prestateFixture))
	require.NoError(t, err)
	ws := converted.WriteSet

	for _, backend := range []wrappers.DBType{wrappers.MemIAVL, wrappers.FlatKV} {
		t.Run(string(backend), func(t *testing.T) {
			wrapper, err := OpenReplayWrapper(t.Context(), backend, t.TempDir())
			require.NoError(t, err)
			defer func() {
				require.NoError(t, wrapper.Close())
			}()

			result, err := ReplayWriteSet(wrapper, ws)
			require.NoError(t, err)
			require.Equal(t, 1, result.Blocks)
			require.Equal(t, ws.TotalKeys(), result.Keys)
			require.Positive(t, result.ApplyDuration)
			require.Positive(t, result.CommitDuration)
			require.Equal(t, int64(1), wrapper.Version())

			// The SSTORE'd slot must be readable back through the store key.
			changesets, err := ws.BlockChangesets(0)
			require.NoError(t, err)
			for _, pair := range changesets[0].Changeset.Pairs {
				if pair.Key[0] == 0x03 {
					value, found, err := wrapper.Read(pair.Key)
					require.NoError(t, err)
					require.True(t, found)
					require.Equal(t, pair.Value, value)
				}
			}
		})
	}
}

func TestLoadWriteSetValidates(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/ws.json"

	require.NoError(t, writeFile(path, `{"blocks": [{"writes": [
      {"kind": "storage", "address": "0x1000000000000000000000000000000000000001",
       "slot": "0x0000000000000000000000000000000000000000000000000000000000000000",
       "value": "0x000000000000000000000000000000000000000000000000000000000000002a"}
    ]}]}`))
	ws, err := LoadWriteSet(path)
	require.NoError(t, err)
	require.Equal(t, 1, ws.TotalKeys())

	require.NoError(t, writeFile(path, `{"blocks": [{"writes": [{"kind": "bogus"}]}]}`))
	_, err = LoadWriteSet(path)
	require.ErrorContains(t, err, "unknown write kind")

	require.NoError(t, writeFile(path, `{"module": "bank", "blocks": [{"writes": []}]}`))
	_, err = LoadWriteSet(path)
	require.ErrorContains(t, err, "unsupported module")
}

func TestValidateRejectsWrongLengthValue(t *testing.T) {
	addr := "0x1000000000000000000000000000000000000001"

	// A wrong-length value for a fixed-width kind is rejected up front, so the
	// benchmark never feeds divergent data to memiavl (permissive) vs FlatKV
	// (which hard-errors on bad lengths deep inside ApplyChangeSets).
	for _, tc := range []struct {
		name  string
		entry WriteSetEntry
	}{
		{"short nonce", WriteSetEntry{Kind: WriteKindNonce, Address: addr, Value: "0x2a000000"}},
		{"short codehash", WriteSetEntry{Kind: WriteKindCodeHash, Address: addr, Value: "0x2a"}},
		{"short storage", WriteSetEntry{Kind: WriteKindStorage, Address: addr,
			Slot: "0x" + hex.EncodeToString(make([]byte, 32)), Value: "0x2a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := &WriteSet{Blocks: []WriteSetBlock{{Writes: []WriteSetEntry{tc.entry}}}}
			err := ws.Validate()
			require.ErrorContains(t, err, "expected")
			_, err = ws.BlockChangesets(0)
			require.ErrorContains(t, err, "expected")
		})
	}

	// Correctly-sized fixed-width values and unconstrained kinds (code/raw) pass.
	ws := &WriteSet{Blocks: []WriteSetBlock{{Writes: []WriteSetEntry{
		{Kind: WriteKindNonce, Address: addr, Value: "0x" + hex.EncodeToString(make([]byte, 8))},
		{Kind: WriteKindCode, Address: addr, Value: "0x602a60005500"},
	}}}}
	require.NoError(t, ws.Validate())
}

func writeFile(path, contents string) error {
	return os.WriteFile(path, []byte(contents), 0o600)
}
