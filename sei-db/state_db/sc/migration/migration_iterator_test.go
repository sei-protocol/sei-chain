package migration

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/stretchr/testify/require"
)

type iteratorFactory func(t *testing.T, data map[string]map[string][]byte, boundary MigrationBoundary) MigrationIterator

func mapFactory(_ *testing.T, data map[string]map[string][]byte, boundary MigrationBoundary) MigrationIterator {
	return NewMapMigrationIterator(data, boundary)
}

func memiavlFactory(t *testing.T, data map[string]map[string][]byte, boundary MigrationBoundary) MigrationIterator {
	t.Helper()
	stores := make([]string, 0, len(data))
	for name := range data {
		stores = append(stores, name)
	}
	db, err := memiavl.OpenDB(0, memiavl.Options{
		Dir:             t.TempDir(),
		CreateIfMissing: true,
		InitialStores:   stores,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var changeSets []*proto.NamedChangeSet
	for name, kvs := range data {
		var pairs []*proto.KVPair
		for k, v := range kvs {
			pairs = append(pairs, &proto.KVPair{Key: []byte(k), Value: v})
		}
		if len(pairs) > 0 {
			changeSets = append(changeSets, &proto.NamedChangeSet{
				Name:      name,
				Changeset: proto.ChangeSet{Pairs: pairs},
			})
		}
	}
	if len(changeSets) > 0 {
		require.NoError(t, db.ApplyChangeSets(changeSets))
	}
	_, err = db.Commit()
	require.NoError(t, err)

	return NewMemiavlMigrationIterator(db, boundary)
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
		require.Equal(t, MigrationBoundaryComplete, boundary)
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
		require.Equal(t, migrationInProgress, boundary.status)
		require.Equal(t, "bank", boundary.moduleName)
		require.Equal(t, []byte("c"), boundary.key)

		batch, boundary, err = iter.NextBatch(10)
		require.NoError(t, err)
		require.Empty(t, batch)
		require.Equal(t, MigrationBoundaryComplete, boundary)
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
		require.Equal(t, migrationInProgress, boundary.status)

		batch, boundary, err = iter.NextBatch(2)
		require.NoError(t, err)
		require.Len(t, batch, 1)
		requireEntry(t, batch[0], "bank", "c", "v3")
		require.Equal(t, migrationInProgress, boundary.status)

		batch, boundary, err = iter.NextBatch(2)
		require.NoError(t, err)
		require.Empty(t, batch)
		require.Equal(t, MigrationBoundaryComplete, boundary)
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
		require.Equal(t, migrationInProgress, boundary.status)
		require.Equal(t, "gov", boundary.moduleName)
		require.Equal(t, []byte("p"), boundary.key)

		batch, boundary, err = iter.NextBatch(3)
		require.NoError(t, err)
		require.Empty(t, batch)
		require.Equal(t, MigrationBoundaryComplete, boundary)
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
		require.Equal(t, migrationInProgress, newBoundary.status)
		require.Equal(t, "gov", newBoundary.moduleName)
		require.Equal(t, []byte("x"), newBoundary.key)

		batch, newBoundary, err = iter.NextBatch(10)
		require.NoError(t, err)
		require.Empty(t, batch)
		require.Equal(t, MigrationBoundaryComplete, newBoundary)
	})

	t.Run("CompleteBoundaryReturnsEmpty", func(t *testing.T) {
		data := map[string]map[string][]byte{
			"bank": {"a": []byte("v1")},
		}
		iter := factory(t, data, MigrationBoundaryComplete)

		batch, boundary, err := iter.NextBatch(10)
		require.NoError(t, err)
		require.Empty(t, batch)
		require.Equal(t, MigrationBoundaryComplete, boundary)
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
		require.Equal(t, MigrationBoundaryComplete, boundary)
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

func requireEntry(t *testing.T, v ValueToMigrate, moduleName, key, value string) {
	t.Helper()
	require.Equal(t, moduleName, v.ModuleName)
	require.Equal(t, []byte(key), v.Key)
	require.Equal(t, []byte(value), v.Value)
}
