package migration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// drainIterator pulls every batch from iter until it reports Complete and
// returns the concatenated, in-order slice of entries.
func drainIterator(t *testing.T, iter *MemiavlMigrationIterator, batchSize int) []ValueToMigrate {
	t.Helper()
	var drained []ValueToMigrate
	for {
		batch, boundary, err := iter.NextBatch(batchSize)
		require.NoError(t, err)
		drained = append(drained, batch...)
		if boundary.Equals(MigrationBoundaryComplete) {
			return drained
		}
		require.NotEmpty(t, batch, "iterator reported in-progress with no batch, would loop forever")
	}
}

// storeNamesIn returns the distinct module names observed in entries, in the
// order they first appear.
func storeNamesIn(entries []ValueToMigrate) []string {
	var names []string
	seen := map[string]struct{}{}
	for _, e := range entries {
		if _, ok := seen[e.ModuleName]; ok {
			continue
		}
		seen[e.ModuleName] = struct{}{}
		names = append(names, e.ModuleName)
	}
	return names
}

func TestMemiavlIterator_WhitelistRestrictsTrees(t *testing.T) {
	data := map[string]map[string][]byte{
		"auth":    {"a": []byte("1"), "b": []byte("2")},
		"bank":    {"c": []byte("3"), "d": []byte("4")},
		"staking": {"e": []byte("5")},
	}

	_, iter := openMemiavlDBFiltered(t, data, []string{"auth", "staking"})
	drained := drainIterator(t, iter, 10)

	// Only whitelisted stores are present, nothing from "bank".
	require.ElementsMatch(t, []string{"auth", "staking"}, storeNamesIn(drained))
	for _, e := range drained {
		require.NotEqual(t, "bank", e.ModuleName,
			"unlisted store %q must not appear in batches", e.ModuleName)
	}

	// Every whitelisted kv was delivered exactly once.
	gotByStore := map[string]map[string]string{}
	for _, e := range drained {
		if gotByStore[e.ModuleName] == nil {
			gotByStore[e.ModuleName] = map[string]string{}
		}
		_, dup := gotByStore[e.ModuleName][string(e.Key)]
		require.False(t, dup, "duplicate entry for %s/%s", e.ModuleName, e.Key)
		gotByStore[e.ModuleName][string(e.Key)] = string(e.Value)
	}
	for _, name := range []string{"auth", "staking"} {
		for k, v := range data[name] {
			require.Equal(t, string(v), gotByStore[name][k],
				"missing or wrong value for %s/%s", name, k)
		}
	}
}

func TestMemiavlIterator_EmptyWhitelistMigratesEverything(t *testing.T) {
	data := map[string]map[string][]byte{
		"auth": {"a": []byte("1")},
		"bank": {"b": []byte("2")},
	}

	// nil and a zero-length slice must behave identically: migrate every store.
	for _, name := range []string{"nil", "empty"} {
		t.Run(name, func(t *testing.T) {
			var whitelist []string
			if name == "empty" {
				whitelist = []string{}
			}
			_, iter := openMemiavlDBFiltered(t, data, whitelist)
			drained := drainIterator(t, iter, 10)
			require.ElementsMatch(t, []string{"auth", "bank"}, storeNamesIn(drained))
			require.Len(t, drained, 2)
		})
	}
}

func TestMemiavlIterator_WhitelistCannotOptInToMigrationStore(t *testing.T) {
	// MigrationStore is reserved for bookkeeping; a caller that explicitly
	// lists it in the whitelist must still never see it in batches.
	data := map[string]map[string][]byte{
		"bank":         {"a": []byte("1")},
		MigrationStore: {"sentinel": []byte("nope")},
	}

	_, iter := openMemiavlDBFiltered(t, data, []string{"bank", MigrationStore})
	drained := drainIterator(t, iter, 10)

	require.Equal(t, []string{"bank"}, storeNamesIn(drained))
	for _, e := range drained {
		require.NotEqual(t, MigrationStore, e.ModuleName)
	}
}

func TestMemiavlIterator_WhitelistSilentlyIgnoresUnknownStores(t *testing.T) {
	// Callers may pass a stable whitelist that names stores not present in
	// this particular DB; unknown entries are skipped without error.
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "b": []byte("2")},
	}

	_, iter := openMemiavlDBFiltered(t, data, []string{"bank", "does-not-exist"})
	drained := drainIterator(t, iter, 10)

	require.Equal(t, []string{"bank"}, storeNamesIn(drained))
	require.Len(t, drained, 2)
}

func TestMemiavlIterator_WhitelistMatchesNoTreesCompletesImmediately(t *testing.T) {
	data := map[string]map[string][]byte{
		"auth": {"a": []byte("1")},
		"bank": {"b": []byte("2")},
	}

	// Whitelist names only stores absent from the DB, so the iterator has
	// nothing to do and must report Complete on the first call without error.
	_, iter := openMemiavlDBFiltered(t, data, []string{"nowhere", "also-nope"})
	batch, boundary, err := iter.NextBatch(10)
	require.NoError(t, err)
	require.Empty(t, batch)
	require.True(t, boundary.Equals(MigrationBoundaryComplete))
}

func TestMemiavlIterator_WhitelistResumeFromBoundary(t *testing.T) {
	// A restart mid-migration should still respect the whitelist: after
	// SetBoundary points somewhere inside an included tree, the iterator
	// picks up at the next key and keeps skipping unlisted trees.
	data := map[string]map[string][]byte{
		"auth":    {"a": []byte("1"), "b": []byte("2")},
		"bank":    {"c": []byte("3")},
		"staking": {"x": []byte("10"), "y": []byte("11")},
	}

	_, iter := openMemiavlDBFiltered(t, data, []string{"auth", "staking"})
	iter.SetBoundary(NewMigrationBoundary("auth", []byte("a")))

	drained := drainIterator(t, iter, 10)

	// "bank" is never touched; "auth/a" is already past the boundary.
	require.Equal(t, []string{"auth", "staking"}, storeNamesIn(drained))
	keysByStore := map[string][]string{}
	for _, e := range drained {
		keysByStore[e.ModuleName] = append(keysByStore[e.ModuleName], string(e.Key))
	}
	require.Equal(t, []string{"b"}, keysByStore["auth"],
		"SetBoundary must skip keys <= boundary.Key within the boundary's module")
	require.Equal(t, []string{"x", "y"}, keysByStore["staking"])
	require.NotContains(t, keysByStore, "bank")
}
