package migration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type iteratorFactory func(t *testing.T, data map[string]map[string][]byte, boundary MigrationBoundary) MigrationIterator

func mapFactory(_ *testing.T, data map[string]map[string][]byte, boundary MigrationBoundary) MigrationIterator {
	return NewMapMigrationIterator(data, boundary)
}

func memiavlFactory(t *testing.T, data map[string]map[string][]byte, boundary MigrationBoundary) MigrationIterator {
	t.Helper()
	_, iter := openMemiavlIterator(t, data, boundary)
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
		iter := factory(t, data, MigrationBoundaryNotStarted)
		batch, boundary, err := iter.NextBatch(10)
		require.NoError(t, err)
		require.Empty(t, batch)
		require.True(t, boundary.Equals(MigrationBoundaryComplete))
	})

	t.Run("SingleModuleBatchFitsAll", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"bank": {"a": []byte("v1"), "b": []byte("v2"), "c": []byte("v3")},
		}
		iter := factory(t, data, MigrationBoundaryNotStarted)

		batch, boundary, err := iter.NextBatch(10)
		require.NoError(t, err)
		require.Len(t, batch, 3)
		requireEntry(t, batch[0], "bank", "a", "v1")
		requireEntry(t, batch[1], "bank", "b", "v2")
		requireEntry(t, batch[2], "bank", "c", "v3")
		require.True(t, boundary.Equals(NewMigrationBoundary("bank", []byte("c"))))

		batch, boundary, err = iter.NextBatch(10)
		require.NoError(t, err)
		require.Empty(t, batch)
		require.True(t, boundary.Equals(MigrationBoundaryComplete))
	})

	t.Run("SingleModuleBatchSmallerThanData", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"bank": {"a": []byte("v1"), "b": []byte("v2"), "c": []byte("v3")},
		}
		iter := factory(t, data, MigrationBoundaryNotStarted)

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
		require.Equal(t, MigrationInProgress, boundary.Status())

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
		iter := factory(t, data, MigrationBoundaryNotStarted)

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
		require.True(t, boundary.Equals(NewMigrationBoundary("gov", []byte("p"))))

		batch, boundary, err = iter.NextBatch(3)
		require.NoError(t, err)
		require.Empty(t, batch)
		require.True(t, boundary.Equals(MigrationBoundaryComplete))
	})

	t.Run("ResumeFromSavedBoundary", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"bank": {"a": []byte("v1"), "b": []byte("v2"), "c": []byte("v3")},
			"gov":  {"x": []byte("gx")},
		}
		boundary := NewMigrationBoundary("bank", []byte("b"))
		iter := factory(t, data, boundary)

		batch, newBoundary, err := iter.NextBatch(10)
		require.NoError(t, err)
		require.Len(t, batch, 2)
		requireEntry(t, batch[0], "bank", "c", "v3")
		requireEntry(t, batch[1], "gov", "x", "gx")
		require.True(t, newBoundary.Equals(NewMigrationBoundary("gov", []byte("x"))))

		batch, newBoundary, err = iter.NextBatch(10)
		require.NoError(t, err)
		require.Empty(t, batch)
		require.True(t, newBoundary.Equals(MigrationBoundaryComplete))
	})

	t.Run("CompleteBoundaryReturnsEmpty", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"bank": {"a": []byte("v1")},
		}
		iter := factory(t, data, MigrationBoundaryComplete)

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
		iter := factory(t, data, MigrationBoundaryNotStarted)

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

	t.Run("ResumeFromModuleBoundaryStart", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"auth": {"z": []byte("az")},
			"bank": {"a": []byte("ba"), "b": []byte("bb")},
		}
		// Boundary is at the last key of "auth", so "bank" should start fresh.
		boundary := NewMigrationBoundary("auth", []byte("z"))
		iter := factory(t, data, boundary)

		batch, _, err := iter.NextBatch(10)
		require.NoError(t, err)
		require.Len(t, batch, 2)
		requireEntry(t, batch[0], "bank", "a", "ba")
		requireEntry(t, batch[1], "bank", "b", "bb")
	})

	t.Run("ResumeFromMiddleOfModule", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"bank": {"a": []byte("v1"), "b": []byte("v2"), "c": []byte("v3"), "d": []byte("v4")},
		}
		boundary := NewMigrationBoundary("bank", []byte("b"))
		iter := factory(t, data, boundary)

		batch, _, err := iter.NextBatch(2)
		require.NoError(t, err)
		require.Len(t, batch, 2)
		requireEntry(t, batch[0], "bank", "c", "v3")
		requireEntry(t, batch[1], "bank", "d", "v4")
	})
}
