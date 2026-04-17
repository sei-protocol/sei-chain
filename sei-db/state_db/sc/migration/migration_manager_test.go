package migration

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// mockDB is a simple in-memory key-value store that records every batch of
// changesets passed to the writer so tests can inspect the raw output.
type mockDB struct {
	data     map[string]map[string][]byte
	writeLog [][]*proto.NamedChangeSet
}

func newMockDB() *mockDB {
	return &mockDB{data: make(map[string]map[string][]byte)}
}

func (db *mockDB) reader() DBReader {
	return func(store string, key []byte) ([]byte, bool, error) {
		if storeData, ok := db.data[store]; ok {
			if val, ok := storeData[string(key)]; ok {
				cp := make([]byte, len(val))
				copy(cp, val)
				return cp, true, nil
			}
		}
		return nil, false, nil
	}
}

func (db *mockDB) writer() DBWriter {
	return func(changesets []*proto.NamedChangeSet) error {
		db.writeLog = append(db.writeLog, changesets)
		for _, cs := range changesets {
			storeData, ok := db.data[cs.Name]
			if !ok {
				storeData = make(map[string][]byte)
				db.data[cs.Name] = storeData
			}
			for _, pair := range cs.Changeset.Pairs {
				if pair.Delete {
					delete(storeData, string(pair.Key))
				} else {
					storeData[string(pair.Key)] = pair.Value
				}
			}
		}
		return nil
	}
}

func (db *mockDB) seed(data map[string]map[string][]byte) {
	for store, kvs := range data {
		storeData, ok := db.data[store]
		if !ok {
			storeData = make(map[string][]byte)
			db.data[store] = storeData
		}
		for k, v := range kvs {
			storeData[k] = v
		}
	}
}

func (db *mockDB) get(store, key string) ([]byte, bool) {
	if storeData, ok := db.data[store]; ok {
		val, ok := storeData[key]
		return val, ok
	}
	return nil, false
}

func failWriter(err error) DBWriter {
	return func(_ []*proto.NamedChangeSet) error { return err }
}

func failReader(err error) DBReader {
	return func(_ string, _ []byte) ([]byte, bool, error) { return nil, false, err }
}

// --- Constructor tests ---

func TestNewMigrationManager_FreshStart(t *testing.T) {
	oldDB := newMockDB()
	newDB := newMockDB()
	iter := NewMapMigrationIterator(map[string]map[string][]byte{
		"bank": {"a": []byte("1")},
	}, false)

	mgr, err := NewMigrationManager(
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 10,
	)
	require.NoError(t, err)
	require.True(t, mgr.boundary.Equals(MigrationBoundaryNotStarted))
}

func TestNewMigrationManager_ResumesFromPersistedBoundary(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "b": []byte("2"), "c": []byte("3")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()

	saved := NewMigrationBoundary("bank", []byte("b"))
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {FlatKVMigrationBoundaryKey: saved.Serialize()},
	})
	iter := NewMapMigrationIterator(copyData(data), false)

	mgr, err := NewMigrationManager(
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 10,
	)
	require.NoError(t, err)
	require.True(t, mgr.boundary.Equals(saved))

	// Should resume past bank/b, only migrating bank/c.
	err = mgr.ApplyChangeSets(nil)
	require.NoError(t, err)

	val, ok := newDB.get("bank", "c")
	require.True(t, ok, "bank/c should have been migrated to newDB")
	require.Equal(t, []byte("3"), val)

	_, ok = newDB.get("bank", "a")
	require.False(t, ok, "bank/a should not have been re-migrated")
	_, ok = newDB.get("bank", "b")
	require.False(t, ok, "bank/b should not have been re-migrated")
}

func TestNewMigrationManager_ReaderError(t *testing.T) {
	oldDB := newMockDB()
	iter := NewMapMigrationIterator(nil, false)

	_, err := NewMigrationManager(
		oldDB.reader(), oldDB.writer(),
		failReader(fmt.Errorf("disk on fire")), oldDB.writer(),
		iter, 10,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "disk on fire")
}

func TestNewMigrationManager_DeserializeError(t *testing.T) {
	newDB := newMockDB()
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {FlatKVMigrationBoundaryKey: []byte("garbage")},
	})
	oldDB := newMockDB()
	iter := NewMapMigrationIterator(nil, false)

	_, err := NewMigrationManager(
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 10,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "deserialize")
}

// --- Read tests ---

func TestRead_RoutesToCorrectDB(t *testing.T) {
	oldDB := newMockDB()
	oldDB.seed(map[string]map[string][]byte{
		"bank": {"a": []byte("old_a"), "z": []byte("old_z")},
	})
	newDB := newMockDB()
	newDB.seed(map[string]map[string][]byte{
		"bank": {"a": []byte("new_a")},
	})

	boundary := NewMigrationBoundary("bank", []byte("m"))
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {FlatKVMigrationBoundaryKey: boundary.Serialize()},
	})
	iter := NewMapMigrationIterator(nil, false)

	mgr, err := NewMigrationManager(
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 10,
	)
	require.NoError(t, err)

	val, ok, err := mgr.Read("bank", []byte("a"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("new_a"), val, "migrated key should come from newDB")

	val, ok, err = mgr.Read("bank", []byte("z"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("old_z"), val, "unmigrated key should come from oldDB")
}

// --- ApplyChangeSets tests ---

func TestApplyChangeSets_MigratesKeysAndPersistsBoundary(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "b": []byte("2"), "c": []byte("3")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()
	iter := NewMapMigrationIterator(copyData(data), false)

	mgr, err := NewMigrationManager(
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 2,
	)
	require.NoError(t, err)

	err = mgr.ApplyChangeSets(nil)
	require.NoError(t, err)

	// bank/a and bank/b migrated to newDB.
	val, ok := newDB.get("bank", "a")
	require.True(t, ok)
	require.Equal(t, []byte("1"), val)
	val, ok = newDB.get("bank", "b")
	require.True(t, ok)
	require.Equal(t, []byte("2"), val)

	// Deleted from oldDB.
	_, ok = oldDB.get("bank", "a")
	require.False(t, ok)
	_, ok = oldDB.get("bank", "b")
	require.False(t, ok)

	// bank/c untouched in oldDB.
	val, ok = oldDB.get("bank", "c")
	require.True(t, ok)
	require.Equal(t, []byte("3"), val)

	// Boundary persisted.
	boundaryBytes, ok := newDB.get(MigrationStore, FlatKVMigrationBoundaryKey)
	require.True(t, ok)
	persisted, err := DeserializeMigrationBoundary(boundaryBytes)
	require.NoError(t, err)
	require.True(t, persisted.Equals(NewMigrationBoundary("bank", []byte("b"))))
}

func TestApplyChangeSets_RoutesIncomingWrites(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "b": []byte("2"), "c": []byte("3")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()
	iter := NewMapMigrationIterator(copyData(data), false)

	mgr, err := NewMigrationManager(
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 2, // migrates bank/a, bank/b → boundary at bank/b
	)
	require.NoError(t, err)

	err = mgr.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("a"), Value: []byte("updated_a")},
		}}},
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("c"), Value: []byte("updated_c")},
		}}},
	})
	require.NoError(t, err)

	// bank/a is migrated → incoming write goes to newDB (overrides migration).
	val, ok := newDB.get("bank", "a")
	require.True(t, ok)
	require.Equal(t, []byte("updated_a"), val)

	// bank/c is not migrated → incoming write goes to oldDB.
	val, ok = oldDB.get("bank", "c")
	require.True(t, ok)
	require.Equal(t, []byte("updated_c"), val)
}

func TestApplyChangeSets_IncomingWriteOverridesMigratedKey(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("original"), "b": []byte("2")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()
	iter := NewMapMigrationIterator(copyData(data), false)

	mgr, err := NewMigrationManager(
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 2,
	)
	require.NoError(t, err)

	err = mgr.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("a"), Value: []byte("overwritten")},
		}}},
	})
	require.NoError(t, err)

	val, ok := newDB.get("bank", "a")
	require.True(t, ok)
	require.Equal(t, []byte("overwritten"), val, "incoming write should win over migrated value")

	val, ok = newDB.get("bank", "b")
	require.True(t, ok)
	require.Equal(t, []byte("2"), val, "uncontested migrated key keeps original value")
}

func TestApplyChangeSets_IncomingDeleteOnMigratedKey(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "b": []byte("2")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()
	iter := NewMapMigrationIterator(copyData(data), false)

	mgr, err := NewMigrationManager(
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 2,
	)
	require.NoError(t, err)

	err = mgr.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("a"), Delete: true},
		}}},
	})
	require.NoError(t, err)

	_, ok := newDB.get("bank", "a")
	require.False(t, ok, "incoming delete should remove migrated key from newDB")

	val, ok := newDB.get("bank", "b")
	require.True(t, ok)
	require.Equal(t, []byte("2"), val)
}

func TestApplyChangeSets_MultiPairChangeSetSplit(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "b": []byte("2"), "c": []byte("3")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()
	iter := NewMapMigrationIterator(copyData(data), false)

	mgr, err := NewMigrationManager(
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 1, // migrates only bank/a → boundary at bank/a
	)
	require.NoError(t, err)

	err = mgr.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("a"), Value: []byte("new_a")},
			{Key: []byte("b"), Value: []byte("new_b")},
			{Key: []byte("c"), Value: []byte("new_c")},
		}}},
	})
	require.NoError(t, err)

	// bank/a is migrated → routed to newDB.
	val, ok := newDB.get("bank", "a")
	require.True(t, ok)
	require.Equal(t, []byte("new_a"), val)

	// bank/b and bank/c are not migrated → routed to oldDB.
	val, ok = oldDB.get("bank", "b")
	require.True(t, ok)
	require.Equal(t, []byte("new_b"), val)
	val, ok = oldDB.get("bank", "c")
	require.True(t, ok)
	require.Equal(t, []byte("new_c"), val)
}

func TestApplyChangeSets_ProducesOneChangeSetPerStore(t *testing.T) {
	data := map[string]map[string][]byte{
		"auth": {"a": []byte("1"), "b": []byte("2")},
		"bank": {"c": []byte("3"), "d": []byte("4")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()
	iter := NewMapMigrationIterator(copyData(data), false)

	mgr, err := NewMigrationManager(
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 10,
	)
	require.NoError(t, err)

	err = mgr.ApplyChangeSets(nil)
	require.NoError(t, err)

	require.Len(t, newDB.writeLog, 1)
	changeSets := newDB.writeLog[0]

	storeCounts := make(map[string]int)
	for _, cs := range changeSets {
		storeCounts[cs.Name]++
	}
	require.Equal(t, 1, storeCounts["auth"])
	require.Equal(t, 1, storeCounts["bank"])
	require.Equal(t, 1, storeCounts[MigrationStore])

	// Stores sorted alphabetically, migration store appended last.
	require.Equal(t, "auth", changeSets[0].Name)
	require.Equal(t, "bank", changeSets[1].Name)
	require.Equal(t, MigrationStore, changeSets[2].Name)

	// Pairs within each store sorted by key.
	require.Len(t, changeSets[0].Changeset.Pairs, 2)
	require.Equal(t, []byte("a"), changeSets[0].Changeset.Pairs[0].Key)
	require.Equal(t, []byte("b"), changeSets[0].Changeset.Pairs[1].Key)

	require.Len(t, changeSets[1].Changeset.Pairs, 2)
	require.Equal(t, []byte("c"), changeSets[1].Changeset.Pairs[0].Key)
	require.Equal(t, []byte("d"), changeSets[1].Changeset.Pairs[1].Key)
}

func TestApplyChangeSets_FullMigration(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank":    {"a": []byte("1"), "b": []byte("2")},
		"staking": {"x": []byte("3")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()
	iter := NewMapMigrationIterator(copyData(data), false)

	mgr, err := NewMigrationManager(
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 2,
	)
	require.NoError(t, err)

	// Call 1: migrates bank/a, bank/b.
	err = mgr.ApplyChangeSets(nil)
	require.NoError(t, err)
	require.Equal(t, MigrationInProgress, mgr.boundary.Status())

	// Call 2: migrates staking/x.
	err = mgr.ApplyChangeSets(nil)
	require.NoError(t, err)
	require.Equal(t, MigrationInProgress, mgr.boundary.Status())

	// Call 3: nothing left.
	err = mgr.ApplyChangeSets(nil)
	require.NoError(t, err)
	require.Equal(t, MigrationComplete, mgr.boundary.Status())

	// All keys now in newDB.
	for store, kvs := range data {
		for k, v := range kvs {
			val, ok := newDB.get(store, k)
			require.True(t, ok, "%s/%s missing from newDB", store, k)
			require.Equal(t, v, val)
		}
	}

	// All keys deleted from oldDB.
	for store, kvs := range data {
		for k := range kvs {
			_, ok := oldDB.get(store, k)
			require.False(t, ok, "%s/%s should be deleted from oldDB", store, k)
		}
	}
}

func TestApplyChangeSets_RecreateManagerResumesWhereLeftOff(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank":    {"a": []byte("1"), "b": []byte("2")},
		"staking": {"x": []byte("3")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()

	// First manager: migrate one batch then "crash" (discard).
	iter1 := NewMapMigrationIterator(copyData(data), false)
	mgr1, err := NewMigrationManager(
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter1, 2,
	)
	require.NoError(t, err)

	err = mgr1.ApplyChangeSets(nil)
	require.NoError(t, err)
	require.Equal(t, MigrationInProgress, mgr1.boundary.Status())

	// Snapshot what newDB looks like after the first batch.
	_, alreadyMigrated := newDB.get("bank", "a")
	require.True(t, alreadyMigrated)

	// Throw away mgr1. Create a brand new manager + iterator from the same DBs.
	iter2 := NewMapMigrationIterator(copyData(data), false)
	mgr2, err := NewMigrationManager(
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter2, 2,
	)
	require.NoError(t, err)
	require.True(t, mgr2.boundary.Equals(mgr1.boundary),
		"new manager should resume from the boundary the old manager persisted")

	// Clear write logs so we can inspect only what the new manager does.
	oldDB.writeLog = nil
	newDB.writeLog = nil

	// Drive the new manager to completion.
	for mgr2.boundary.Status() != MigrationComplete {
		err = mgr2.ApplyChangeSets(nil)
		require.NoError(t, err)
	}

	// The new manager should NOT have re-migrated bank/a or bank/b.
	for _, batch := range newDB.writeLog {
		for _, cs := range batch {
			if cs.Name == "bank" {
				for _, pair := range cs.Changeset.Pairs {
					require.NotEqual(t, "a", string(pair.Key),
						"bank/a was already migrated; should not appear again")
					require.NotEqual(t, "b", string(pair.Key),
						"bank/b was already migrated; should not appear again")
				}
			}
		}
	}

	// All keys should now be in newDB.
	for store, kvs := range data {
		for k, v := range kvs {
			val, ok := newDB.get(store, k)
			require.True(t, ok, "%s/%s missing from newDB", store, k)
			require.Equal(t, v, val)
		}
	}
}

func TestApplyChangeSets_AfterMigrationComplete(t *testing.T) {
	oldDB := newMockDB()
	newDB := newMockDB()
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {FlatKVMigrationBoundaryKey: MigrationBoundaryComplete.Serialize()},
	})
	iter := NewMapMigrationIterator(nil, false)

	mgr, err := NewMigrationManager(
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 10,
	)
	require.NoError(t, err)
	require.Equal(t, MigrationComplete, mgr.boundary.Status())

	err = mgr.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("a"), Value: []byte("val")},
		}}},
	})
	require.NoError(t, err)

	val, ok := newDB.get("bank", "a")
	require.True(t, ok)
	require.Equal(t, []byte("val"), val, "all writes should go to newDB when migration is complete")

	// Old writer was called but with no changesets.
	require.Len(t, oldDB.writeLog, 1)
	require.Empty(t, oldDB.writeLog[0])
}

func TestApplyChangeSets_OldWriterError(t *testing.T) {
	data := map[string]map[string][]byte{"bank": {"a": []byte("1")}}
	newDB := newMockDB()
	iter := NewMapMigrationIterator(copyData(data), false)

	mgr, err := NewMigrationManager(
		newMockDB().reader(), failWriter(fmt.Errorf("old disk full")),
		newDB.reader(), newDB.writer(),
		iter, 10,
	)
	require.NoError(t, err)

	err = mgr.ApplyChangeSets(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "old disk full")
}

func TestApplyChangeSets_NewWriterError(t *testing.T) {
	data := map[string]map[string][]byte{"bank": {"a": []byte("1")}}
	oldDB := newMockDB()
	iter := NewMapMigrationIterator(copyData(data), false)

	mgr, err := NewMigrationManager(
		oldDB.reader(), oldDB.writer(),
		newMockDB().reader(), failWriter(fmt.Errorf("new disk full")),
		iter, 10,
	)
	require.NoError(t, err)

	err = mgr.ApplyChangeSets(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "new disk full")
}

func TestMigrationManagerRandomized(t *testing.T) {
	seed := time.Now().UnixNano()
	t.Logf("random seed: %d", seed)
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec

	storeNames := []string{"auth", "bank", "gov", "staking"}
	numKeysPerStore := 50
	allKeys := make([]string, numKeysPerStore)
	for i := range allKeys {
		allKeys[i] = fmt.Sprintf("key_%03d", i)
	}

	// Build initial data.
	reference := make(map[string]map[string][]byte, len(storeNames))
	for _, store := range storeNames {
		reference[store] = make(map[string][]byte)
		for _, key := range allKeys {
			reference[store][key] = fuzzRandomBytes(rng, 8)
		}
	}

	// Old DB gets a deep copy. The iterator shares oldDB.data so that
	// Rebuild picks up mutations made by the DB writer.
	oldDB := newMockDB()
	oldDB.seed(copyData(reference))
	newDB := newMockDB()

	iter := NewMapMigrationIterator(oldDB.data, true)

	mgr, err := NewMigrationManager(
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 10,
	)
	require.NoError(t, err)

	// Run random rounds of mutations interspersed with migration progress.
	numRounds := 100
	for round := 0; round < numRounds; round++ {
		changesets := fuzzRandomChangesets(rng, storeNames, numKeysPerStore)

		fuzzApplyToReference(reference, changesets)

		err := mgr.ApplyChangeSets(changesets)
		require.NoError(t, err, "round %d", round)

		// Every read through the migration manager must match the reference.
		for _, store := range storeNames {
			for _, key := range allKeys {
				expected, exists := reference[store][key]
				actual, ok, readErr := mgr.Read(store, []byte(key))
				require.NoError(t, readErr, "round %d: read %s/%s", round, store, key)
				if exists {
					require.True(t, ok, "round %d: %s/%s should exist", round, store, key)
					require.Equal(t, expected, actual, "round %d: %s/%s value mismatch", round, store, key)
				} else {
					require.False(t, ok, "round %d: %s/%s should not exist", round, store, key)
				}
			}
		}
	}

	// Drive migration to completion with no further mutations.
	for mgr.boundary.Status() != MigrationComplete {
		err := mgr.ApplyChangeSets(nil)
		require.NoError(t, err)
	}

	// Final verification: all reads still match after migration is complete.
	for _, store := range storeNames {
		for _, key := range allKeys {
			expected, exists := reference[store][key]
			actual, ok, readErr := mgr.Read(store, []byte(key))
			require.NoError(t, readErr, "final: read %s/%s", store, key)
			if exists {
				require.True(t, ok, "final: %s/%s should exist", store, key)
				require.Equal(t, expected, actual, "final: %s/%s value mismatch", store, key)
			} else {
				require.False(t, ok, "final: %s/%s should not exist", store, key)
			}
		}
	}
}

func fuzzRandomBytes(rng *rand.Rand, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Intn(256))
	}
	return b
}

func fuzzRandomChangesets(rng *rand.Rand, stores []string, numKeys int) []*proto.NamedChangeSet {
	numOps := rng.Intn(5) + 1
	byStore := make(map[string][]*proto.KVPair)
	for i := 0; i < numOps; i++ {
		store := stores[rng.Intn(len(stores))]
		key := fmt.Sprintf("key_%03d", rng.Intn(numKeys))
		if rng.Float64() < 0.2 {
			byStore[store] = append(byStore[store], &proto.KVPair{Key: []byte(key), Delete: true})
		} else {
			byStore[store] = append(byStore[store], &proto.KVPair{Key: []byte(key), Value: fuzzRandomBytes(rng, 8)})
		}
	}

	names := make([]string, 0, len(byStore))
	for name := range byStore {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]*proto.NamedChangeSet, 0, len(names))
	for _, name := range names {
		result = append(result, &proto.NamedChangeSet{
			Name:      name,
			Changeset: proto.ChangeSet{Pairs: byStore[name]},
		})
	}
	return result
}

func fuzzApplyToReference(ref map[string]map[string][]byte, changesets []*proto.NamedChangeSet) {
	for _, cs := range changesets {
		storeData := ref[cs.Name]
		if storeData == nil {
			storeData = make(map[string][]byte)
			ref[cs.Name] = storeData
		}
		for _, pair := range cs.Changeset.Pairs {
			if pair.Delete {
				delete(storeData, string(pair.Key))
			} else {
				storeData[string(pair.Key)] = pair.Value
			}
		}
	}
}
