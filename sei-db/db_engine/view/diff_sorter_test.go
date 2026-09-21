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
	shardDiffs := [][]diffEntry{
		{{key: "a"}, {key: "d"}, {key: "z"}},
		{},
		{{key: "b"}, {key: "e"}},
		{{key: "c"}},
	}

	var got []string
	require.NoError(t, forEachMergedEntry(shardDiffs, func(entry diffEntry) error {
		got = append(got, entry.key)
		return nil
	}))

	require.Equal(t, []string{"a", "b", "c", "d", "e", "z"}, got)
}

// Values ride along with their keys, tombstones included: a nil value is what tells the encoder to
// write a delete rather than a set.
func TestForEachMergedEntryCarriesValuesAndTombstones(t *testing.T) {
	shardDiffs := [][]diffEntry{
		{{key: "gone", value: nil}},
		{{key: "empty", value: []byte{}}, {key: "set", value: []byte("v")}},
	}

	got := make(map[string][]byte)
	require.NoError(t, forEachMergedEntry(shardDiffs, func(entry diffEntry) error {
		got[entry.key] = entry.value
		return nil
	}))

	require.Len(t, got, 3)
	require.Nil(t, got["gone"], "a tombstone must stay nil through the merge")
	require.NotNil(t, got["empty"], "an empty value must stay distinguishable from a tombstone")
	require.Empty(t, got["empty"])
	require.Equal(t, []byte("v"), got["set"])
}

func TestForEachMergedEntryHandlesNothingToMerge(t *testing.T) {
	require.NoError(t, forEachMergedEntry(nil, func(diffEntry) error {
		t.Fatal("nothing to visit")
		return nil
	}))
	require.NoError(t, forEachMergedEntry([][]diffEntry{{}, {}}, func(diffEntry) error {
		t.Fatal("nothing to visit")
		return nil
	}))
}

// The encoder reports a failed write through the visitor, and the merge has to stop there rather than
// carry on filling a batch that is already broken.
func TestForEachMergedEntryStopsAtTheFirstVisitError(t *testing.T) {
	shardDiffs := [][]diffEntry{{{key: "a"}, {key: "c"}}, {{key: "b"}}}
	failure := errors.New("encode failed")

	visited := 0
	err := forEachMergedEntry(shardDiffs, func(diffEntry) error {
		visited++
		if visited == 2 {
			return failure
		}
		return nil
	})

	require.ErrorIs(t, err, failure)
	require.Equal(t, 2, visited, "the merge must stop at the entry that failed")
}
