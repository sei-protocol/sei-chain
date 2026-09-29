package view

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// The merge is what gives the flush one ascending sequence out of the per-shard diffs, so it has to
// interleave them by key rather than concatenate them, and it has to cope with diffs that are empty or
// exhausted at different points.
func TestForEachMergedEntryInterleavesRunsByKey(t *testing.T) {
	shardDiffs := [][]Write[[]byte]{
		{{Key: "a"}, {Key: "d"}, {Key: "z"}},
		{},
		{{Key: "b"}, {Key: "e"}},
		{{Key: "c"}},
	}

	var got []string
	require.NoError(t, forEachMergedEntry(shardDiffs, func(entry Write[[]byte]) error {
		got = append(got, entry.Key)
		return nil
	}))

	require.Equal(t, []string{"a", "b", "c", "d", "e", "z"}, got)
}

// Values ride along with their keys, deletes included: the flag is what tells the encoder to write a
// delete rather than a set.
func TestForEachMergedEntryCarriesValuesAndTombstones(t *testing.T) {
	shardDiffs := [][]Write[[]byte]{
		{{Key: "gone", Delete: true}},
		{{Key: "empty", Value: []byte{}}, {Key: "set", Value: []byte("v")}},
	}

	got := make(map[string]Write[[]byte])
	require.NoError(t, forEachMergedEntry(shardDiffs, func(entry Write[[]byte]) error {
		got[entry.Key] = entry
		return nil
	}))

	require.Len(t, got, 3)
	require.True(t, got["gone"].Delete, "a delete must stay a delete through the merge")
	require.False(t, got["empty"].Delete, "an empty value must stay distinguishable from a delete")
	require.Empty(t, got["empty"].Value)
	require.Equal(t, []byte("v"), got["set"].Value)
}

func TestForEachMergedEntryHandlesNothingToMerge(t *testing.T) {
	require.NoError(t, forEachMergedEntry(nil, func(Write[[]byte]) error {
		t.Fatal("nothing to visit")
		return nil
	}))
	require.NoError(t, forEachMergedEntry([][]Write[[]byte]{{}, {}}, func(Write[[]byte]) error {
		t.Fatal("nothing to visit")
		return nil
	}))
}

// The encoder reports a failed write through the visitor, and the merge has to stop there rather than
// carry on filling a batch that is already broken.
func TestForEachMergedEntryStopsAtTheFirstVisitError(t *testing.T) {
	shardDiffs := [][]Write[[]byte]{{{Key: "a"}, {Key: "c"}}, {{Key: "b"}}}
	failure := errors.New("encode failed")

	visited := 0
	err := forEachMergedEntry(shardDiffs, func(Write[[]byte]) error {
		visited++
		if visited == 2 {
			return failure
		}
		return nil
	})

	require.ErrorIs(t, err, failure)
	require.Equal(t, 2, visited, "the merge must stop at the entry that failed")
}
