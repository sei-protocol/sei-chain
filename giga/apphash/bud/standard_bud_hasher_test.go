package bud

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// A BUD hash reported by a BUDHasher.
type reportedBUDHash struct {
	blockHeight uint64
	budHash     [32]byte
}

// recordBUDReports returns a report function that sends each BUD hash to the returned channel, which holds up to
// capacity of them.
func recordBUDReports(
	capacity int,
) (func(context.Context, uint64, [32]byte) error, <-chan reportedBUDHash) {
	reported := make(chan reportedBUDHash, capacity)
	report := func(_ context.Context, blockHeight uint64, budHash [32]byte) error {
		reported <- reportedBUDHash{blockHeight: blockHeight, budHash: budHash}
		return nil
	}
	return report, reported
}

// newBUDHasher starts a StandardBUDHasher with the default config and the given buffer size.
func newBUDHasher(
	t *testing.T,
	bufferSize int,
	report func(context.Context, uint64, [32]byte) error,
) *StandardBUDHasher {
	t.Helper()
	config := DefaultBUDHasherConfig()
	config.BufferSize = bufferSize
	hasher, err := NewStandardBUDHasher(t.Context(), config, report)
	require.NoError(t, err)
	return hasher
}

// insertsOf returns a mutation inserting the value of each key, in the order given.
func insertsOf(keys ...string) []mutation.Mutation {
	mutations := make([]mutation.Mutation, len(keys))
	for i, key := range keys {
		mutations[i] = mutation.Mutation{Key: []byte(key), NewValue: vtype.SerializeMisc(9, []byte("v-"+key))}
	}
	return mutations
}

// budHashOfSorted returns the BUD hash of mutations once sorted by key.
func budHashOfSorted(t *testing.T, mutations []mutation.Mutation) [32]byte {
	t.Helper()
	sorted := slices.Clone(mutations)
	slices.SortFunc(sorted, func(a mutation.Mutation, b mutation.Mutation) int {
		return bytes.Compare(a.Key, b.Key)
	})
	hash, err := budHashFromMutations(sorted)
	require.NoError(t, err)
	return hash
}

// mustSerializeStorage returns the storage value holding value, written at blockHeight.
func mustSerializeStorage(t *testing.T, blockHeight int64, value []byte) []byte {
	t.Helper()
	serialized, err := vtype.SerializeStorage(blockHeight, value)
	require.NoError(t, err)
	return serialized
}

func TestBUDHasherReportsEveryBlockInOrderBeforeClosing(t *testing.T) {
	report, reported := recordBUDReports(3)
	hasher := newBUDHasher(t, 3, report)

	blocks := [][]mutation.Mutation{insertsOf("a"), nil, insertsOf("c", "a", "b")}
	for i, mutations := range blocks {
		blockHeight := uint64(10 + i) //nolint:gosec // small test heights
		require.NoError(t, hasher.Submit(t.Context(), blockHeight, mutations))
	}
	require.NoError(t, hasher.Close())

	require.Len(t, reported, len(blocks))
	for i, mutations := range blocks {
		blockHeight := uint64(10 + i) //nolint:gosec // small test heights
		want := reportedBUDHash{blockHeight: blockHeight, budHash: budHashOfSorted(t, mutations)}
		require.Equal(t, want, <-reported)
	}
}

// The hasher sorts its own copy of a block, so the hash does not depend on the order the mutations were submitted
// in, and the caller's slice is left as it was.
func TestBUDHasherSortsACopyOfTheBlock(t *testing.T) {
	report, reported := recordBUDReports(2)
	hasher := newBUDHasher(t, 2, report)

	forward := insertsOf("a", "b", "c", "d")
	reverse := insertsOf("d", "c", "b", "a")
	require.NoError(t, hasher.Submit(t.Context(), 1, forward))
	require.NoError(t, hasher.Submit(t.Context(), 2, reverse))
	require.NoError(t, hasher.Close())

	require.Equal(t, (<-reported).budHash, (<-reported).budHash)
	require.Equal(t, insertsOf("d", "c", "b", "a"), reverse)
}

func TestBUDHasherStopsOnMutationsItCannotHash(t *testing.T) {
	report, reported := recordBUDReports(1)
	hasher := newBUDHasher(t, 1, report)

	require.NoError(t, hasher.Submit(t.Context(), 1, []mutation.Mutation{{Key: []byte{}}}))

	require.ErrorContains(t, hasher.Close(), "failed to hash block 1")
	require.ErrorContains(t, hasher.Submit(t.Context(), 2, nil), "BUD hasher stopped")
	require.Empty(t, reported)
}

func TestBUDHasherStopsWhenAReportFails(t *testing.T) {
	reportErr := errors.New("builder stopped")
	hasher := newBUDHasher(t, 1, func(context.Context, uint64, [32]byte) error {
		return reportErr
	})

	require.NoError(t, hasher.Submit(t.Context(), 1, nil))

	err := hasher.Close()
	require.ErrorIs(t, err, reportErr)
	require.ErrorContains(t, err, "failed to report the BUD hash of block 1")
	require.ErrorIs(t, hasher.Submit(t.Context(), 2, nil), reportErr)
}

func TestBUDHasherSubmitGivesUpWhenItsContextIsDone(t *testing.T) {
	release := make(chan struct{})
	reporting := make(chan struct{}, 1)
	hasher := newBUDHasher(t, 1, func(context.Context, uint64, [32]byte) error {
		select {
		case reporting <- struct{}{}:
		default:
		}
		<-release
		return nil
	})

	// Block 1 holds the hashing goroutine, so the blocks behind it eventually fill both queues.
	require.NoError(t, hasher.Submit(t.Context(), 1, nil))
	<-reporting
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var err error
	for blockHeight := uint64(2); err == nil; blockHeight++ {
		err = hasher.Submit(ctx, blockHeight, nil)
	}
	require.ErrorIs(t, err, context.Canceled)

	close(release)
	require.NoError(t, hasher.Close())
}

func TestBUDHasherRefusesAnInvalidConfig(t *testing.T) {
	report, _ := recordBUDReports(0)
	_, err := NewStandardBUDHasher(t.Context(), BUDHasherConfig{BufferSize: 0}, report)
	require.ErrorContains(t, err, "buffer size must be positive")
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

func TestBUDHashFromNoMutationsIsTheEmptyTree(t *testing.T) {
	hash, err := budHashFromMutations(nil)
	require.NoError(t, err)
	const emptyTreeBUDHash = "9725d028d186ff14d54a60658eac9ea435e5bace76d9aa01df155e4e89f1678f"
	require.Equal(t, emptyTreeBUDHash, hex.EncodeToString(hash[:]))
}

func TestBUDHashFromMutationsRejectsKeysOutOfOrder(t *testing.T) {
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
			_, err := budHashFromMutations(mutations)
			require.Error(t, err)
		})
	}
}

// The BUD hash commits to every mutation: changing any one changes it.
func TestBUDHashFromMutationsDependsOnEveryMutation(t *testing.T) {
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
	baseline, err := budHashFromMutations(mutations())
	require.NoError(t, err)

	edits := []func(m []mutation.Mutation){
		func(m []mutation.Mutation) { m[0].NewValue = vtype.SerializeMisc(9, []byte("x")) },
		func(m []mutation.Mutation) { m[1].PreviousValue = vtype.SerializeMisc(2, []byte("2")) },
		func(m []mutation.Mutation) { m[1].NewValue = nil },
	}
	for i, edit := range edits {
		edited := mutations()
		edit(edited)
		hash, err := budHashFromMutations(edited)
		require.NoError(t, err)
		require.NotEqual(t, baseline, hash, "edit %d", i)
	}
}
