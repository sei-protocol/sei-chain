package statewal

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// mustSerializeStorage returns the storage value holding value, written at blockHeight.
func mustSerializeStorage(t *testing.T, blockHeight int64, value []byte) []byte {
	t.Helper()
	serialized, err := vtype.SerializeStorage(blockHeight, value)
	require.NoError(t, err)
	return serialized
}

func TestBudletFromMutation(t *testing.T) {
	slot := bytes.Repeat([]byte{0xab}, vtype.StorageValueLength)
	previousSlot := bytes.Repeat([]byte{0xcd}, vtype.StorageValueLength)

	tests := []struct {
		name                     string
		mutation                 mutation.Mutation
		expectedValue            []byte
		expectedPreviousValue    []byte
		expectedNotModifiedSince uint64
	}{
		{
			name: "insert",
			mutation: mutation.Mutation{
				Key:      []byte("key"),
				NewValue: vtype.SerializeMisc(9, []byte("new")),
			},
			expectedValue:            []byte("\x00new"),
			expectedPreviousValue:    []byte{},
			expectedNotModifiedSince: budTombstoneHorizon,
		},
		{
			name: "update",
			mutation: mutation.Mutation{
				Key:           []byte("key"),
				PreviousValue: vtype.SerializeMisc(4, []byte("old")),
				NewValue:      vtype.SerializeMisc(9, []byte("new")),
			},
			expectedValue:            []byte("\x00new"),
			expectedPreviousValue:    []byte("\x00old"),
			expectedNotModifiedSince: 4,
		},
		{
			name: "delete",
			mutation: mutation.Mutation{
				Key:           []byte("key"),
				PreviousValue: vtype.SerializeMisc(4, []byte("old")),
			},
			expectedValue:            []byte{},
			expectedPreviousValue:    []byte("\x00old"),
			expectedNotModifiedSince: 4,
		},
		{
			name:                     "delete of an absent key",
			mutation:                 mutation.Mutation{Key: []byte("key")},
			expectedValue:            []byte{},
			expectedPreviousValue:    []byte{},
			expectedNotModifiedSince: budTombstoneHorizon,
		},
		{
			name: "empty value is not the absent payload",
			mutation: mutation.Mutation{
				Key:           []byte("key"),
				PreviousValue: vtype.SerializeMisc(4, nil),
			},
			expectedValue:            []byte{},
			expectedPreviousValue:    []byte{0x00},
			expectedNotModifiedSince: 4,
		},
		{
			name: "storage value",
			mutation: mutation.Mutation{
				Key:           []byte("key"),
				PreviousValue: mustSerializeStorage(t, 4, previousSlot),
				NewValue:      mustSerializeStorage(t, 9, slot),
			},
			expectedValue:            append([]byte{0x00}, slot...),
			expectedPreviousValue:    append([]byte{0x00}, previousSlot...),
			expectedNotModifiedSince: 4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budlet, err := budletFromMutation(&tt.mutation)
			require.NoError(t, err)
			require.Equal(t, tt.mutation.Key, budlet.Key())
			require.Equal(t, string(tt.expectedValue), string(budlet.Value()))
			require.Equal(t, string(tt.expectedPreviousValue), string(budlet.PreviousValue()))
			require.Equal(t, tt.expectedNotModifiedSince, budlet.NotModifiedSince())
		})
	}
}

func TestBudletFromMutationRejectsInvalidMutations(t *testing.T) {
	shortValue := vtype.SerializeMisc(4, nil)[:vtype.VersionLength+vtype.BlockHeightLength-1]
	tests := []struct {
		name     string
		mutation mutation.Mutation
	}{
		{
			name:     "empty key",
			mutation: mutation.Mutation{Key: []byte{}, NewValue: vtype.SerializeMisc(9, []byte("new"))},
		},
		{
			name:     "new value shorter than a value header",
			mutation: mutation.Mutation{Key: []byte("key"), NewValue: shortValue},
		},
		{
			name:     "previous value shorter than a value header",
			mutation: mutation.Mutation{Key: []byte("key"), PreviousValue: shortValue},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := budletFromMutation(&tt.mutation)
			require.Error(t, err)
		})
	}
}

// The budlets of the BUD spec's test vector that a FlatKV block can produce, built from misc values.
func TestBudletFromMutationMatchesSpecVector(t *testing.T) {
	tests := []struct {
		name               string
		mutation           mutation.Mutation
		expectedSerialized string
	}{
		{
			name: "b[0]",
			mutation: mutation.Mutation{
				Key:           []byte("evm/a"),
				PreviousValue: vtype.SerializeMisc(0, []byte{0xaa}),
				NewValue:      vtype.SerializeMisc(9, []byte{0xaa, 0xbb}),
			},
			expectedSerialized: "00000005" + "65766d2f61" + "00000003" + "00aabb" + "00000002" + "00aa" +
				"0000000000000000",
		},
		{
			name: "b[1]",
			mutation: mutation.Mutation{
				Key:           []byte("evm/b"),
				PreviousValue: vtype.SerializeMisc(7, []byte{0xbb}),
				NewValue:      vtype.SerializeMisc(9, []byte{0xbb, 0xcc}),
			},
			expectedSerialized: "00000005" + "65766d2f62" + "00000003" + "00bbcc" + "00000002" + "00bb" +
				"0000000000000007",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budlet, err := budletFromMutation(&tt.mutation)
			require.NoError(t, err)
			require.Equal(t, tt.expectedSerialized, hex.EncodeToString(budlet.Serialize()))
		})
	}
}

func TestBUDTreeFromNoMutationsIsTheEmptyTree(t *testing.T) {
	tree, err := budTreeFromMutations(nil)
	require.NoError(t, err)
	hash := tree.BUDHash()
	const emptyTreeBUDHash = "3eeeba2dfb311dad9e9e46eba984fa855be2b872496b921b19da52b3eb09d5e2"
	require.Equal(t, emptyTreeBUDHash, hex.EncodeToString(hash[:]))
}

func TestBUDTreeFromMutationsRejectsKeysOutOfOrder(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		{name: "descending", keys: []string{"b", "a"}},
		{name: "duplicate", keys: []string{"a", "a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutations := make([]mutation.Mutation, len(tt.keys))
			for i, key := range tt.keys {
				mutations[i] = mutation.Mutation{
					Key:      []byte(key),
					NewValue: vtype.SerializeMisc(9, []byte("v")),
				}
			}
			_, err := budTreeFromMutations(mutations)
			require.Error(t, err)
		})
	}
}

// The BUD hash commits to every mutation: changing any one changes it.
func TestBUDTreeFromMutationsHashDependsOnEveryMutation(t *testing.T) {
	mutations := func() []mutation.Mutation {
		return []mutation.Mutation{
			{Key: []byte("a"), NewValue: vtype.SerializeMisc(9, []byte("1"))},
			{
				Key:           []byte("b"),
				PreviousValue: vtype.SerializeMisc(3, []byte("2")),
				NewValue:      vtype.SerializeMisc(9, nil),
			},
		}
	}
	baseline, err := budTreeFromMutations(mutations())
	require.NoError(t, err)

	edits := []func(m []mutation.Mutation){
		func(m []mutation.Mutation) { m[0].NewValue = vtype.SerializeMisc(9, []byte("x")) },
		func(m []mutation.Mutation) { m[1].PreviousValue = vtype.SerializeMisc(2, []byte("2")) },
		func(m []mutation.Mutation) { m[1].NewValue = nil },
	}
	for i, edit := range edits {
		edited := mutations()
		edit(edited)
		tree, err := budTreeFromMutations(edited)
		require.NoError(t, err)
		require.NotEqual(t, baseline.BUDHash(), tree.BUDHash(), "edit %d", i)
	}
}

// The hash a WAL delivers and iterates for a block is the BUD hash of the block's mutations, before and after a
// reopen.
func TestHashIsBUDHashOfMutations(t *testing.T) {
	cfg := testConfig(t.TempDir())
	blocks := [][]mutation.Mutation{
		{makeMutation("bank", []byte("a"), []byte("1"))},
		{makeMutation("bank", []byte("b"), nil), makeMutation("staking", []byte("c"), []byte("2"))},
		{},
	}
	want := make([]recordedHash, 0, len(blocks))
	for i, mutations := range blocks {
		tree, err := budTreeFromMutations(mutations)
		require.NoError(t, err)
		want = append(want, recordedHash{blockNumber: uint64(i + 1), hash: tree.BUDHash()})
	}

	w := openWAL(t, cfg)
	recorder := &hashRecorder{}
	_, _, _, err := w.RegisterHashListener(recorder.listen)
	require.NoError(t, err)
	for i, mutations := range blocks {
		require.NoError(t, w.Write(uint64(i+1), mutations))
	}
	require.NoError(t, w.Flush())
	require.Equal(t, want, recorder.deliveries)
	require.Equal(t, want, iteratedHashes(t, w, 1, uint64(len(blocks))))
	require.NoError(t, w.Close())

	reopened := openWAL(t, cfg)
	defer func() { require.NoError(t, reopened.Close()) }()
	require.Equal(t, want, iteratedHashes(t, reopened, 1, uint64(len(blocks))))
	ok, last, lastHash, err := reopened.RegisterHashListener(nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(len(blocks)), last)
	require.Equal(t, want[len(want)-1].hash, lastHash)
}
