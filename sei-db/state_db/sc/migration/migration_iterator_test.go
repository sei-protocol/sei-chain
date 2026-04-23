package migration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type iteratorFactory func(t *testing.T, data map[string]map[string][]byte) MigrationIterator

func mapFactory(_ *testing.T, data map[string]map[string][]byte) MigrationIterator {
	return NewMockMigrationIterator(data, false)
}

func memiavlFactory(t *testing.T, data map[string]map[string][]byte) MigrationIterator {
	t.Helper()
	_, iter := openMemiavlDB(t, data)
	return iter
}

func TestMigrationIterator(t *testing.T) {
	factories := map[string]iteratorFactory{
		"map":     mapFactory,
		"memiavl": memiavlFactory,
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			runMigrationIteratorTests(t, factory)
		})
	}
}

func runMigrationIteratorTests(t *testing.T, factory iteratorFactory) {
	t.Run("EmptyData", func(t *testing.T) {
		data := map[string]map[string][]byte{}
		iter := factory(t, data)
		batch, boundary, err := iter.NextBatch(10)
		require.NoError(t, err)
		require.Empty(t, batch)
		require.True(t, boundary.Equals(MigrationBoundaryComplete))
	})

	t.Run("SingleModuleBatchFitsAll", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"bank": {"a": []byte("v1"), "b": []byte("v2"), "c": []byte("v3")},
		}
		iter := factory(t, data)

		batch, boundary, err := iter.NextBatch(10)
		require.NoError(t, err)
		require.Len(t, batch, 3)
		requireEntry(t, batch[0], "bank", "a", "v1")
		requireEntry(t, batch[1], "bank", "b", "v2")
		requireEntry(t, batch[2], "bank", "c", "v3")
		require.True(t, boundary.Equals(MigrationBoundaryComplete),
			"iterator should report Complete eagerly on the batch that drains it")

		batch, boundary, err = iter.NextBatch(10)
		require.NoError(t, err)
		require.Empty(t, batch)
		require.True(t, boundary.Equals(MigrationBoundaryComplete))
	})

	t.Run("SingleModuleBatchSmallerThanData", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"bank": {"a": []byte("v1"), "b": []byte("v2"), "c": []byte("v3")},
		}
		iter := factory(t, data)

		batch, boundary, err := iter.NextBatch(2)
		require.NoError(t, err)
		require.Len(t, batch, 2)
		requireEntry(t, batch[0], "bank", "a", "v1")
		requireEntry(t, batch[1], "bank", "b", "v2")
		require.Equal(t, MigrationInProgress, boundary.Status())

		batch, boundary, err = iter.NextBatch(2)
		require.NoError(t, err)
		require.Len(t, batch, 1)
		requireEntry(t, batch[0], "bank", "c", "v3")
		require.True(t, boundary.Equals(MigrationBoundaryComplete),
			"iterator should report Complete eagerly on the batch that drains it")

		batch, boundary, err = iter.NextBatch(2)
		require.NoError(t, err)
		require.Empty(t, batch)
		require.True(t, boundary.Equals(MigrationBoundaryComplete))
	})

	t.Run("MultipleModulesBatchSpansBoundary", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"auth": {"x": []byte("ax")},
			"bank": {"a": []byte("ba"), "b": []byte("bb")},
			"gov":  {"p": []byte("gp")},
		}
		iter := factory(t, data)

		batch, _, err := iter.NextBatch(3)
		require.NoError(t, err)
		require.Len(t, batch, 3)
		requireEntry(t, batch[0], "auth", "x", "ax")
		requireEntry(t, batch[1], "bank", "a", "ba")
		requireEntry(t, batch[2], "bank", "b", "bb")

		batch, boundary, err := iter.NextBatch(3)
		require.NoError(t, err)
		require.Len(t, batch, 1)
		requireEntry(t, batch[0], "gov", "p", "gp")
		require.True(t, boundary.Equals(MigrationBoundaryComplete),
			"iterator should report Complete eagerly on the batch that drains it")

		batch, boundary, err = iter.NextBatch(3)
		require.NoError(t, err)
		require.Empty(t, batch)
		require.True(t, boundary.Equals(MigrationBoundaryComplete))
	})

	t.Run("SetBoundaryResumesFromMiddle", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"bank": {"a": []byte("v1"), "b": []byte("v2"), "c": []byte("v3")},
			"gov":  {"x": []byte("gx")},
		}
		iter := factory(t, data)
		iter.SetBoundary(NewMigrationBoundary("bank", []byte("b")))

		batch, newBoundary, err := iter.NextBatch(10)
		require.NoError(t, err)
		require.Len(t, batch, 2)
		requireEntry(t, batch[0], "bank", "c", "v3")
		requireEntry(t, batch[1], "gov", "x", "gx")
		require.True(t, newBoundary.Equals(MigrationBoundaryComplete),
			"iterator should report Complete eagerly on the batch that drains it")

		batch, newBoundary, err = iter.NextBatch(10)
		require.NoError(t, err)
		require.Empty(t, batch)
		require.True(t, newBoundary.Equals(MigrationBoundaryComplete))
	})

	t.Run("SetBoundaryCompleteReturnsEmpty", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"bank": {"a": []byte("v1")},
		}
		iter := factory(t, data)
		iter.SetBoundary(MigrationBoundaryComplete)

		batch, boundary, err := iter.NextBatch(10)
		require.NoError(t, err)
		require.Empty(t, batch)
		require.True(t, boundary.Equals(MigrationBoundaryComplete))
	})

	t.Run("BatchSizeOne", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"auth": {"k1": []byte("v1")},
			"bank": {"k2": []byte("v2")},
			"gov":  {"k3": []byte("v3")},
		}
		iter := factory(t, data)

		expected := []struct {
			mod, key, val string
		}{
			{"auth", "k1", "v1"},
			{"bank", "k2", "v2"},
			{"gov", "k3", "v3"},
		}
		for _, exp := range expected {
			batch, _, err := iter.NextBatch(1)
			require.NoError(t, err)
			require.Len(t, batch, 1)
			requireEntry(t, batch[0], exp.mod, exp.key, exp.val)
		}

		batch, boundary, err := iter.NextBatch(1)
		require.NoError(t, err)
		require.Empty(t, batch)
		require.True(t, boundary.Equals(MigrationBoundaryComplete))
	})

	t.Run("SetBoundaryToModuleBoundary", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"auth": {"z": []byte("az")},
			"bank": {"a": []byte("ba"), "b": []byte("bb")},
		}
		iter := factory(t, data)
		iter.SetBoundary(NewMigrationBoundary("auth", []byte("z")))

		batch, _, err := iter.NextBatch(10)
		require.NoError(t, err)
		require.Len(t, batch, 2)
		requireEntry(t, batch[0], "bank", "a", "ba")
		requireEntry(t, batch[1], "bank", "b", "bb")
	})

	t.Run("SetBoundaryToMiddleOfModule", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"bank": {"a": []byte("v1"), "b": []byte("v2"), "c": []byte("v3"), "d": []byte("v4")},
		}
		iter := factory(t, data)
		iter.SetBoundary(NewMigrationBoundary("bank", []byte("b")))

		batch, _, err := iter.NextBatch(2)
		require.NoError(t, err)
		require.Len(t, batch, 2)
		requireEntry(t, batch[0], "bank", "c", "v3")
		requireEntry(t, batch[1], "bank", "d", "v4")
	})

	t.Run("SetBoundaryAfterPartialIteration", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"bank": {"a": []byte("v1"), "b": []byte("v2"), "c": []byte("v3"), "d": []byte("v4")},
		}
		iter := factory(t, data)

		batch, boundary, err := iter.NextBatch(2)
		require.NoError(t, err)
		require.Len(t, batch, 2)
		requireEntry(t, batch[0], "bank", "a", "v1")
		requireEntry(t, batch[1], "bank", "b", "v2")

		// Rewind to before "b" by setting boundary to "a".
		iter.SetBoundary(NewMigrationBoundary("bank", []byte("a")))

		batch, boundary, err = iter.NextBatch(10)
		require.NoError(t, err)
		require.Len(t, batch, 3)
		requireEntry(t, batch[0], "bank", "b", "v2")
		requireEntry(t, batch[1], "bank", "c", "v3")
		requireEntry(t, batch[2], "bank", "d", "v4")
		require.True(t, boundary.Equals(MigrationBoundaryComplete),
			"iterator should report Complete eagerly on the batch that drains it")
	})

	t.Run("NextBatchZeroReturnsError", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"bank": {"a": []byte("v1")},
		}
		iter := factory(t, data)

		_, boundary, err := iter.NextBatch(0)
		require.Error(t, err)
		require.Contains(t, err.Error(), "batch size must be positive")
		require.True(t, boundary.Equals(MigrationBoundaryNotStarted),
			"boundary must not advance on error")

		// A subsequent valid call must still work (no state corruption).
		batch, _, err := iter.NextBatch(10)
		require.NoError(t, err)
		require.Len(t, batch, 1)
		requireEntry(t, batch[0], "bank", "a", "v1")
	})

	t.Run("NextBatchNegativeReturnsError", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"bank": {"a": []byte("v1")},
		}
		iter := factory(t, data)

		_, boundary, err := iter.NextBatch(-5)
		require.Error(t, err)
		require.Contains(t, err.Error(), "batch size must be positive")
		require.True(t, boundary.Equals(MigrationBoundaryNotStarted),
			"boundary must not advance on error")
	})

	t.Run("SetBoundaryNotStartedResetsToBeginning", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"bank": {"a": []byte("v1"), "b": []byte("v2")},
		}
		iter := factory(t, data)

		// Iterate everything.
		batch, _, err := iter.NextBatch(10)
		require.NoError(t, err)
		require.Len(t, batch, 2)

		// Confirm exhausted.
		batch, _, err = iter.NextBatch(10)
		require.NoError(t, err)
		require.Empty(t, batch)

		// Reset to beginning.
		iter.SetBoundary(MigrationBoundaryNotStarted)

		batch, _, err = iter.NextBatch(10)
		require.NoError(t, err)
		require.Len(t, batch, 2)
		requireEntry(t, batch[0], "bank", "a", "v1")
		requireEntry(t, batch[1], "bank", "b", "v2")
	})

	t.Run("SkipsMigrationStore", func(t *testing.T) {
		// Migration metadata lives in MigrationStore and must never be
		// migrated. The iterator should behave as if that store doesn't
		// exist, regardless of its lexicographic position relative to
		// real stores.
		data := map[string]map[string][]byte{
			"auth":         {"a": []byte("v1"), "b": []byte("v2")},
			"zeta":         {"z": []byte("v3")},
			MigrationStore: {"secret": []byte("do-not-migrate")},
		}
		iter := factory(t, data)

		batch, boundary, err := iter.NextBatch(10)
		require.NoError(t, err)
		require.Len(t, batch, 3)
		for _, entry := range batch {
			require.NotEqual(t, MigrationStore, entry.ModuleName,
				"MigrationStore entries must not appear in migration output")
		}
		requireEntry(t, batch[0], "auth", "a", "v1")
		requireEntry(t, batch[1], "auth", "b", "v2")
		requireEntry(t, batch[2], "zeta", "z", "v3")
		require.True(t, boundary.Equals(MigrationBoundaryComplete),
			"iterator should report Complete eagerly on the batch that drains it, "+
				"even when MigrationStore would have been next in order")

		// A subsequent call still reports Complete, not landing the
		// MigrationStore entry.
		batch, boundary, err = iter.NextBatch(10)
		require.NoError(t, err)
		require.Empty(t, batch)
		require.True(t, boundary.Equals(MigrationBoundaryComplete))
	})

	t.Run("SkipsMigrationStoreWhenOnlyStore", func(t *testing.T) {
		// A DB containing nothing but MigrationStore has nothing to
		// migrate. The iterator should report completion on the first
		// NextBatch call.
		data := map[string]map[string][]byte{
			MigrationStore: {"secret": []byte("do-not-migrate")},
		}
		iter := factory(t, data)

		batch, boundary, err := iter.NextBatch(10)
		require.NoError(t, err)
		require.Empty(t, batch)
		require.True(t, boundary.Equals(MigrationBoundaryComplete))
	})
}
