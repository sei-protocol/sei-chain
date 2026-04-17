package migration

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/stretchr/testify/require"
)

// openMemiavlDB creates a memiavl DB populated with the given data and returns
// the DB (for applying mutations) along with a MemiavlMigrationIterator.
func openMemiavlDB(
	t *testing.T,
	data map[string]map[string][]byte,
) (*memiavl.DB, *MemiavlMigrationIterator) {
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

	return db, NewMemiavlMigrationIterator(db)
}

// copyData returns a deep copy of a nested map so mock and memiavl start from
// identical but independent data.
func copyData(data map[string]map[string][]byte) map[string]map[string][]byte {
	out := make(map[string]map[string][]byte, len(data))
	for mod, kvs := range data {
		m := make(map[string][]byte, len(kvs))
		for k, v := range kvs {
			vc := make([]byte, len(v))
			copy(vc, v)
			m[k] = vc
		}
		out[mod] = m
	}
	return out
}

// requireBatchesEqual asserts two batches have identical entries in order.
func requireBatchesEqual(t *testing.T, a, b []ValueToMigrate) {
	t.Helper()
	require.Equal(t, len(a), len(b), "batch lengths differ")
	for i := range a {
		require.Equal(t, a[i].ModuleName, b[i].ModuleName, "ModuleName mismatch at index %d", i)
		require.Equal(t, a[i].Key, b[i].Key, "Key mismatch at index %d", i)
		require.Equal(t, a[i].Value, b[i].Value, "Value mismatch at index %d", i)
	}
}

// requireEntry asserts a single ValueToMigrate matches the expected strings.
func requireEntry(t *testing.T, v ValueToMigrate, moduleName, key, value string) {
	t.Helper()
	require.Equal(t, moduleName, v.ModuleName)
	require.Equal(t, []byte(key), v.Key)
	require.Equal(t, []byte(value), v.Value)
}
