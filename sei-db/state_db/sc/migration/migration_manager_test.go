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
	return func(changesets []*proto.NamedChangeSet, _ bool) error {
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
	return func(_ []*proto.NamedChangeSet, _ bool) error { return err }
}

func failReader(err error) DBReader {
	return func(_ string, _ []byte) ([]byte, bool, error) { return nil, false, err }
}

// Default version boundaries used by newTestManager. Tests that need to
// exercise other values call NewMigrationManager directly.
const (
	testStartVersion  = 0
	testTargetVersion = 1
)

// newTestManager wraps NewMigrationManager for tests, supplying the
// default version boundaries (start=0, target=1). Argument order mirrors
// NewMigrationManager modulo batch size and versions, which are moved
// to the start of the real API signature.
func newTestManager(
	t *testing.T,
	oldReader DBReader, oldWriter DBWriter,
	newReader DBReader, newWriter DBWriter,
	iter MigrationIterator,
	size int,
) (*MigrationManager, error) {
	t.Helper()
	return NewMigrationManager(
		size,
		testStartVersion, testTargetVersion,
		oldReader, oldWriter, newReader, newWriter, iter,
		nil,
	)
}

// --- Constructor tests ---

func TestNewMigrationManager_FreshStart(t *testing.T) {
	oldDB := newMockDB()
	newDB := newMockDB()
	iter := NewMockMigrationIterator(map[string]map[string][]byte{
		"bank": {"a": []byte("1")},
	}, false)

	mgr, err := newTestManager(t,
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
		MigrationStore: {MigrationBoundaryKey: saved.Serialize()},
	})
	iter := NewMockMigrationIterator(copyData(data), false)

	mgr, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 10,
	)
	require.NoError(t, err)
	require.True(t, mgr.boundary.Equals(saved))

	// Should resume past bank/b, only migrating bank/c.
	err = mgr.ApplyChangeSets(nil, true)
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
	iter := NewMockMigrationIterator(nil, false)

	_, err := newTestManager(t,
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
		MigrationStore: {MigrationBoundaryKey: []byte("garbage")},
	})
	oldDB := newMockDB()
	iter := NewMockMigrationIterator(nil, false)

	_, err := newTestManager(t,
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
		MigrationStore: {MigrationBoundaryKey: boundary.Serialize()},
	})
	iter := NewMockMigrationIterator(nil, false)

	mgr, err := newTestManager(t,
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
	iter := NewMockMigrationIterator(copyData(data), false)

	mgr, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 2,
	)
	require.NoError(t, err)

	err = mgr.ApplyChangeSets(nil, true)
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
	boundaryBytes, ok := newDB.get(MigrationStore, MigrationBoundaryKey)
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
	iter := NewMockMigrationIterator(copyData(data), false)

	mgr, err := newTestManager(t,
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
	}, true)
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
	iter := NewMockMigrationIterator(copyData(data), false)

	mgr, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 2,
	)
	require.NoError(t, err)

	err = mgr.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("a"), Value: []byte("overwritten")},
		}}},
	}, true)
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
	iter := NewMockMigrationIterator(copyData(data), false)

	mgr, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 2,
	)
	require.NoError(t, err)

	err = mgr.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("a"), Delete: true},
		}}},
	}, true)
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
	iter := NewMockMigrationIterator(copyData(data), false)

	mgr, err := newTestManager(t,
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
	}, true)
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
	iter := NewMockMigrationIterator(copyData(data), false)

	mgr, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 10,
	)
	require.NoError(t, err)

	err = mgr.ApplyChangeSets(nil, true)
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
	iter := NewMockMigrationIterator(copyData(data), false)

	mgr, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 2,
	)
	require.NoError(t, err)

	// Call 1: migrates bank/a, bank/b.
	err = mgr.ApplyChangeSets(nil, true)
	require.NoError(t, err)
	require.Equal(t, MigrationInProgress, mgr.boundary.Status())
	require.False(t, mgr.boundary.Equals(MigrationBoundaryComplete))

	// Call 2: migrates staking/x — the last entry, so the iterator
	// reports Complete and the manager finalizes in the same call.
	err = mgr.ApplyChangeSets(nil, true)
	require.NoError(t, err)
	require.Equal(t, MigrationComplete, mgr.boundary.Status())
	require.True(t, mgr.boundary.Equals(MigrationBoundaryComplete))

	// All keys now in newDB.
	for store, kvs := range data {
		for k, v := range kvs {
			val, ok := newDB.get(store, k)
			require.True(t, ok, "%s/%s missing from newDB", store, k)
			require.Equal(t, v, val)
		}
	}

	// All migrated keys were deleted from oldDB. The final call also
	// fans its migration deletes out to the old DB just like any other
	// migrating call — callers that want to keep the old DB intact are
	// responsible for tearing down the old DB handle themselves.
	_, ok := oldDB.get("bank", "a")
	require.False(t, ok)
	_, ok = oldDB.get("bank", "b")
	require.False(t, ok)
	_, ok = oldDB.get("staking", "x")
	require.False(t, ok, "final call still issues migration deletes to old DB")

	stats := mgr.metrics.RunStats()
	require.Equal(t, int64(2), stats.batches)
	require.Equal(t, int64(3), stats.keysMigrated)
	require.Equal(t, int64(3), stats.keyBytesMigrated)
	require.Equal(t, int64(3), stats.valueBytesMigrated)
	require.Equal(t, int64(0), stats.originalPairsRoutedOldDB)
	require.Equal(t, int64(0), stats.originalPairsRoutedNewDB)
	require.Equal(t, int64(3), stats.oldDBPairsWritten)
	require.Equal(t, int64(6), stats.newDBPairsWritten)
}

func TestApplyChangeSets_RecreateManagerResumesWhereLeftOff(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank":    {"a": []byte("1"), "b": []byte("2")},
		"staking": {"x": []byte("3")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()

	// First manager: migrate one batch then "crash" (discard). The
	// persisted boundary in the new DB is the only state the second
	// manager needs to resume.
	iter1 := NewMockMigrationIterator(copyData(data), false)
	mgr1, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter1, 2,
	)
	require.NoError(t, err)

	err = mgr1.ApplyChangeSets(nil, true)
	require.NoError(t, err)
	require.Equal(t, MigrationInProgress, mgr1.boundary.Status())

	// Snapshot what newDB looks like after the first batch.
	_, alreadyMigrated := newDB.get("bank", "a")
	require.True(t, alreadyMigrated)

	// Throw away mgr1. Create a brand new manager + iterator against the
	// same DBs; it should pick up the persisted boundary.
	iter2 := NewMockMigrationIterator(copyData(data), false)
	mgr2, err := newTestManager(t,
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
		err = mgr2.ApplyChangeSets(nil, true)
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
	iter := NewMockMigrationIterator(nil, false)

	mgr, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 10,
	)
	require.NoError(t, err)

	// Drive the manager into the post-completion state directly so
	// this test focuses on the ApplyChangeSets fast path in isolation.
	// (The constructor will produce this state on its own when the new
	// DB already reports targetVersion - see
	// TestNewMigrationManager_AcceptsNewDBAtTargetVersion - but here we
	// just want to exercise the post-completion branch without setting
	// up that fixture.)
	mgr.boundary = MigrationBoundaryComplete

	changesets := []*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("a"), Value: []byte("val")},
		}}},
	}
	err = mgr.ApplyChangeSets(changesets, true)
	require.NoError(t, err)

	val, ok := newDB.get("bank", "a")
	require.True(t, ok)
	require.Equal(t, []byte("val"), val, "all writes should go to newDB after migration is complete")

	// Post-completion: new DB receives the caller's changesets verbatim
	// (no injected MigrationStore boundary re-write) and the old DB
	// writer is not called at all.
	require.Empty(t, oldDB.writeLog, "old DB writer should not be called after final block")
	require.Len(t, newDB.writeLog, 1)
	require.Equal(t, changesets, newDB.writeLog[0])
}

func TestApplyChangeSets_AfterMigrationCompleteNilChangesets(t *testing.T) {
	oldDB := newMockDB()
	newDB := newMockDB()
	iter := NewMockMigrationIterator(nil, false)

	mgr, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 10,
	)
	require.NoError(t, err)

	// Drive the manager into the post-completion state directly so
	// this test focuses on the nil-changeset post-completion fast
	// path in isolation.
	mgr.boundary = MigrationBoundaryComplete

	err = mgr.ApplyChangeSets(nil, true)
	require.NoError(t, err)

	require.Empty(t, oldDB.writeLog, "old DB writer should not be called after final block")
	require.Len(t, newDB.writeLog, 1)
	require.Empty(t, newDB.writeLog[0])
}

func TestApplyChangeSets_OldWriterError(t *testing.T) {
	// Two keys + batch size 1 so the first call is mid-migration and
	// actually invokes the old-DB writer (the final call skips it).
	data := map[string]map[string][]byte{"bank": {"a": []byte("1"), "b": []byte("2")}}
	newDB := newMockDB()
	iter := NewMockMigrationIterator(copyData(data), false)

	mgr, err := newTestManager(t,
		newMockDB().reader(), failWriter(fmt.Errorf("old disk full")),
		newDB.reader(), newDB.writer(),
		iter, 1,
	)
	require.NoError(t, err)

	err = mgr.ApplyChangeSets(nil, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "old disk full")
}

func TestApplyChangeSets_NewWriterError(t *testing.T) {
	data := map[string]map[string][]byte{"bank": {"a": []byte("1")}}
	oldDB := newMockDB()
	iter := NewMockMigrationIterator(copyData(data), false)

	mgr, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newMockDB().reader(), failWriter(fmt.Errorf("new disk full")),
		iter, 10,
	)
	require.NoError(t, err)

	err = mgr.ApplyChangeSets(nil, true)
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

	iter := NewMockMigrationIterator(oldDB.data, true)

	mgr, err := newTestManager(t,
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

		err := mgr.ApplyChangeSets(changesets, true)
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
		err := mgr.ApplyChangeSets(nil, true)
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

// --- Constructor validation (Issue 2, Issue 11) ---

// A batch size of 0 is valid: the migration manager comes up paused and
// advances no keys until SetMigrationBatchSize raises it above 0.
func TestNewMigrationManager_AcceptsZeroBatchSize(t *testing.T) {
	oldDB := newMockDB()
	newDB := newMockDB()
	iter := NewMockMigrationIterator(nil, false)

	m, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 0,
	)
	require.NoError(t, err)
	require.NotNil(t, m)
}

func TestNewMigrationManager_NilDependencies(t *testing.T) {
	iter := NewMockMigrationIterator(nil, false)
	oldDB := newMockDB()
	newDB := newMockDB()

	validReader := oldDB.reader()
	validWriter := oldDB.writer()
	validNewReader := newDB.reader()
	validNewWriter := newDB.writer()

	cases := []struct {
		name         string
		oldReader    DBReader
		oldWriter    DBWriter
		newReader    DBReader
		newWriter    DBWriter
		iter         MigrationIterator
		wantContains string
	}{
		{"nil oldDBReader", nil, validWriter, validNewReader, validNewWriter, iter, "oldDBReader"},
		{"nil oldDBWriter", validReader, nil, validNewReader, validNewWriter, iter, "oldDBWriter"},
		{"nil newDBReader", validReader, validWriter, nil, validNewWriter, iter, "newDBReader"},
		{"nil newDBWriter", validReader, validWriter, validNewReader, nil, iter, "newDBWriter"},
		{"nil iterator", validReader, validWriter, validNewReader, validNewWriter, nil, "iterator"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newTestManager(t,
				tc.oldReader, tc.oldWriter,
				tc.newReader, tc.newWriter,
				tc.iter, 10,
			)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantContains)
		})
	}
}

// TestNewMigrationManager_AcceptsNewDBAtTargetVersion pins the contract
// that the constructor accepts a new DB whose version already equals
// targetVersion and produces a manager that comes up in passthrough
// mode: boundary = Complete, every read routes to the new DB, and
// ApplyChangeSets takes the post-completion fast path. This is what
// keeps a migration-mode WriteMode safe to leave configured
// indefinitely after the migration completes - operators don't need
// to flip a config setting on the first restart past migration completion.
func TestNewMigrationManager_AcceptsNewDBAtTargetVersion(t *testing.T) {
	oldDB := newMockDB()
	oldDB.seed(map[string]map[string][]byte{
		"bank": {"k": []byte("from-old")},
	})
	newDB := newMockDB()
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(testTargetVersion)},
		"bank":         {"k": []byte("from-new")},
	})
	iter := NewMockMigrationIterator(nil, false)

	mgr, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 10,
	)
	require.NoError(t, err)
	require.NotNil(t, mgr)
	require.True(t, mgr.boundary.Equals(MigrationBoundaryComplete),
		"manager constructed at targetVersion must come up with boundary = Complete")

	// Reads must route to the new DB (the migrated side) for every
	// store; if the manager were treating the boundary as NotStarted
	// the read below would surface "from-old" from the old DB.
	val, ok, err := mgr.Read("bank", []byte("k"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("from-new"), val,
		"passthrough manager must read migrated keys from the new DB")

	// Writes must take the post-completion fast path: forwarded
	// verbatim to the new DB, old DB untouched, no migration
	// bookkeeping injected.
	cs := []*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k2"), Value: []byte("v2")},
		}}},
	}
	require.NoError(t, mgr.ApplyChangeSets(cs, true))
	val, ok = newDB.get("bank", "k2")
	require.True(t, ok)
	require.Equal(t, []byte("v2"), val)
	require.Empty(t, oldDB.writeLog,
		"old DB must not be written when manager comes up post-completion")
}

// --- Issue 7: old-DB changeset grouping ---

func TestApplyChangeSets_OldDBChangeSetGroupedByStore(t *testing.T) {
	data := map[string]map[string][]byte{
		"auth": {"a": []byte("1"), "b": []byte("2"), "c": []byte("3")},
		"bank": {"x": []byte("X"), "y": []byte("Y")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()
	iter := NewMockMigrationIterator(copyData(data), false)

	// batch=2 -> migrates auth/a, auth/b; boundary sits at auth/b.
	mgr, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 2,
	)
	require.NoError(t, err)

	// Multiple incoming pairs per store, across two stores, all unmigrated
	// (below and after the boundary respectively). Combined with migration
	// deletes, we should still see exactly one old-DB changeset per store.
	err = mgr.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "auth", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("c"), Value: []byte("updated_c")},
		}}},
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("x"), Value: []byte("updated_x")},
			{Key: []byte("y"), Value: []byte("updated_y")},
		}}},
	}, true)
	require.NoError(t, err)

	require.Len(t, oldDB.writeLog, 1)
	oldCS := oldDB.writeLog[0]

	// One NamedChangeSet per store, sorted by name. The old DB sees no
	// MigrationStore entries: the manager owns those keys only on the
	// new-DB side.
	require.Len(t, oldCS, 2)
	require.Equal(t, "auth", oldCS[0].Name)
	require.Equal(t, "bank", oldCS[1].Name)

	// auth: deletes for a, b (migration deletes, iterator order) followed
	// by the incoming pair for c.
	require.Len(t, oldCS[0].Changeset.Pairs, 3)
	require.Equal(t, []byte("a"), oldCS[0].Changeset.Pairs[0].Key)
	require.True(t, oldCS[0].Changeset.Pairs[0].Delete)
	require.Equal(t, []byte("b"), oldCS[0].Changeset.Pairs[1].Key)
	require.True(t, oldCS[0].Changeset.Pairs[1].Delete)
	require.Equal(t, []byte("c"), oldCS[0].Changeset.Pairs[2].Key)
	require.False(t, oldCS[0].Changeset.Pairs[2].Delete)
	require.Equal(t, []byte("updated_c"), oldCS[0].Changeset.Pairs[2].Value)

	// bank: no migration deletes yet (boundary is at auth/b), only the
	// two incoming pairs in input order.
	require.Len(t, oldCS[1].Changeset.Pairs, 2)
	require.Equal(t, []byte("x"), oldCS[1].Changeset.Pairs[0].Key)
	require.Equal(t, []byte("updated_x"), oldCS[1].Changeset.Pairs[0].Value)
	require.Equal(t, []byte("y"), oldCS[1].Changeset.Pairs[1].Key)
	require.Equal(t, []byte("updated_y"), oldCS[1].Changeset.Pairs[1].Value)
}

// --- Issue 9: MigrationStore routing & rejection ---

func TestRead_MigrationStoreRejected(t *testing.T) {
	// MigrationStore is reserved for the manager's bookkeeping. Any
	// Read targeting it must fail fast, regardless of boundary state
	// or whether the migration has finished.
	oldDB := newMockDB()
	newDB := newMockDB()
	const sentinelKey = "sentinel"
	oldDB.seed(map[string]map[string][]byte{
		MigrationStore: {sentinelKey: []byte("old-value")},
	})
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {sentinelKey: []byte("new-value")},
	})

	iter := NewMockMigrationIterator(nil, false)
	mgr, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 10,
	)
	require.NoError(t, err)

	cases := []struct {
		name     string
		boundary MigrationBoundary
	}{
		{"NotStarted", MigrationBoundaryNotStarted},
		{"InProgress_beforeMigration", NewMigrationBoundary("auth", []byte("x"))},
		{"InProgress_afterMigration", NewMigrationBoundary("zzzzz", []byte("x"))},
		{"Complete", MigrationBoundaryComplete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr.boundary = tc.boundary
			val, ok, err := mgr.Read(MigrationStore, []byte(sentinelKey))
			require.Error(t, err)
			require.Contains(t, err.Error(), "migration")
			require.Nil(t, val)
			require.False(t, ok)
		})
	}
}

func TestApplyChangeSets_RejectsMigrationStoreWrites(t *testing.T) {
	oldDB := newMockDB()
	newDB := newMockDB()
	iter := NewMockMigrationIterator(map[string]map[string][]byte{
		"bank": {"a": []byte("1")},
	}, false)

	mgr, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter, 10,
	)
	require.NoError(t, err)

	err = mgr.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: MigrationStore, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("x"), Value: []byte("y")},
		}}},
	}, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), MigrationStore)

	// No writes should have reached either DB.
	require.Empty(t, oldDB.writeLog, "old DB writer must not run when request is rejected")
	require.Empty(t, newDB.writeLog, "new DB writer must not run when request is rejected")
}

// TestApplyChangeSets_OldDBErrorAbortsNewDBWrite pins the sequential
// contract that an old-DB write failure must prevent the new-DB write
// from being attempted. This is the partner correctness invariant to
// the "old DB first, then new DB" ordering: if the old-DB write blew
// up, we want a clean recovery point with the new DB still untouched
// rather than a torn cross-DB state.
func TestApplyChangeSets_OldDBErrorAbortsNewDBWrite(t *testing.T) {
	data := map[string]map[string][]byte{"bank": {"a": []byte("1"), "b": []byte("2")}}

	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()
	iter := NewMockMigrationIterator(copyData(data), false)

	sentinel := fmt.Errorf("old-DB boom")
	var newWriterCalled bool
	mgr, err := newTestManager(t,
		oldDB.reader(), failWriter(sentinel),
		newDB.reader(),
		func(_ []*proto.NamedChangeSet, _ bool) error {
			newWriterCalled = true
			return nil
		},
		iter, 1,
	)
	require.NoError(t, err)

	err = mgr.ApplyChangeSets(nil, true)
	require.ErrorIs(t, err, sentinel)
	require.False(t, newWriterCalled,
		"old-DB writer error must abort sequential dispatch before the new-DB writer runs")
}

// --- BuildRoute tests ---

// inProgressManager builds a valid mid-migration MigrationManager and
// returns it alongside the backing oldDB and newDB so tests can seed
// and assert on them. The BuildRoute tests below only exercise the
// manager as a Router, so a no-op iterator (no source data) is fine;
// the manager simply does not migrate any keys per call. The boundary
// starts at MigrationBoundaryNotStarted; tests that need a particular
// boundary state mutate it directly.
func inProgressManager(t *testing.T) (*MigrationManager, *mockDB, *mockDB) {
	t.Helper()
	oldDB := newMockDB()
	newDB := newMockDB()
	mgr, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMockMigrationIterator(nil, false), 10,
	)
	require.NoError(t, err)
	return mgr, oldDB, newDB
}

func TestBuildRoute_ReturnsValidRoute(t *testing.T) {
	mgr, _, _ := inProgressManager(t)
	route, err := mgr.BuildRoute("evm", "bank")
	require.NoError(t, err)
	require.NotNil(t, route)
	require.NotNil(t, route.reader)
	require.NotNil(t, route.writer)
	require.NotNil(t, route.proofBuilder)
}

func TestBuildRoute_DuplicateModuleNamesRejected(t *testing.T) {
	// BuildRoute must propagate NewRoute's duplicate-module validation
	// rather than swallowing it; misconfiguration should fail loudly.
	mgr, _, _ := inProgressManager(t)
	route, err := mgr.BuildRoute("evm", "bank", "evm")
	require.Error(t, err)
	require.Nil(t, route)
	require.Contains(t, err.Error(), "evm")
	require.Contains(t, err.Error(), "more than once")
}

func TestBuildRoute_EmptyModulesAllowed(t *testing.T) {
	// Mirrors NewRoute: a route with no modules is valid (just receives
	// no traffic), and BuildRoute must not impose stricter rules.
	mgr, _, _ := inProgressManager(t)
	route, err := mgr.BuildRoute()
	require.NoError(t, err)
	require.NotNil(t, route)
}

func TestBuildRoute_ReaderDispatchesThroughManager_PostCompletion(t *testing.T) {
	mgr, oldDB, newDB := inProgressManager(t)
	// Drive the manager into the post-completion state directly so we
	// can prove the reader routes everything to the new DB. If
	// BuildRoute incorrectly wired the reader to oldDBReader, we'd see
	// "from-old" here instead.
	mgr.boundary = MigrationBoundaryComplete
	newDB.seed(map[string]map[string][]byte{"bank": {"k": []byte("from-new")}})
	oldDB.seed(map[string]map[string][]byte{"bank": {"k": []byte("from-old")}})

	route, err := mgr.BuildRoute("bank")
	require.NoError(t, err)

	val, ok, err := route.reader("bank", []byte("k"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("from-new"), val,
		"BuildRoute must wire the reader through MigrationManager.Read, not directly to oldDBReader")
}

func TestBuildRoute_ReaderRespectsMigrationBoundary(t *testing.T) {
	// Mid-migration: keys at-or-before the boundary live in newDB,
	// keys past the boundary still live in oldDB. The route's reader
	// must split the same way the manager's Read does.
	oldDB := newMockDB()
	oldDB.seed(map[string]map[string][]byte{
		"bank": {"a": []byte("old_a"), "z": []byte("old_z")},
	})
	newDB := newMockDB()
	boundary := NewMigrationBoundary("bank", []byte("m"))
	newDB.seed(map[string]map[string][]byte{
		"bank":         {"a": []byte("new_a")},
		MigrationStore: {MigrationBoundaryKey: boundary.Serialize()},
	})
	mgr, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMockMigrationIterator(nil, false), 10,
	)
	require.NoError(t, err)

	route, err := mgr.BuildRoute("bank")
	require.NoError(t, err)

	val, ok, err := route.reader("bank", []byte("a"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("new_a"), val, "migrated key must read from newDB through the route")

	val, ok, err = route.reader("bank", []byte("z"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("old_z"), val, "unmigrated key must read from oldDB through the route")
}

func TestBuildRoute_ReaderRejectsMigrationStore(t *testing.T) {
	// Reads against MigrationStore are reserved for the manager itself.
	// The route's reader must surface the same rejection — proving
	// that the route really is wired through MigrationManager.Read and
	// not straight to a raw DBReader.
	mgr, _, _ := inProgressManager(t)
	route, err := mgr.BuildRoute(MigrationStore)
	require.NoError(t, err)

	val, ok, err := route.reader(MigrationStore, []byte("anything"))
	require.Error(t, err)
	require.False(t, ok)
	require.Nil(t, val)
	require.Contains(t, err.Error(), "migration")
}

func TestBuildRoute_WriterDispatchesThroughManager_PostCompletion(t *testing.T) {
	mgr, oldDB, newDB := inProgressManager(t)
	// Drive the manager into the post-completion state directly so the
	// writer path is the simple "forward to new DB only" branch.
	mgr.boundary = MigrationBoundaryComplete
	route, err := mgr.BuildRoute("bank")
	require.NoError(t, err)

	cs := []*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k"), Value: []byte("v")},
		}}},
	}
	require.NoError(t, route.writer(cs, true))

	val, ok := newDB.get("bank", "k")
	require.True(t, ok)
	require.Equal(t, []byte("v"), val,
		"BuildRoute must wire the writer through MigrationManager.ApplyChangeSets")
	require.Empty(t, oldDB.writeLog, "old DB writer must not be invoked after final block")
}

func TestBuildRoute_WriterMidMigrationDrivesMigration(t *testing.T) {
	// Mid-migration: every ApplyChangeSets call should advance the
	// migration. If BuildRoute had wired its writer directly to the
	// raw oldDBWriter, no migration step would happen and the boundary
	// would never advance.
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "b": []byte("2"), "c": []byte("3")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()
	mgr, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMockMigrationIterator(copyData(data), false), 2, // batch advances boundary to bank/b
	)
	require.NoError(t, err)

	route, err := mgr.BuildRoute("bank")
	require.NoError(t, err)
	require.NoError(t, route.writer(nil, true))

	val, ok := newDB.get("bank", "a")
	require.True(t, ok)
	require.Equal(t, []byte("1"), val)
	val, ok = newDB.get("bank", "b")
	require.True(t, ok)
	require.Equal(t, []byte("2"), val)
	_, ok = oldDB.get("bank", "a")
	require.False(t, ok, "migrated key must be deleted from oldDB")

	boundaryBytes, ok := newDB.get(MigrationStore, MigrationBoundaryKey)
	require.True(t, ok)
	persisted, err := DeserializeMigrationBoundary(boundaryBytes)
	require.NoError(t, err)
	require.True(t, persisted.Equals(NewMigrationBoundary("bank", []byte("b"))),
		"writer must advance the boundary; if it bypassed the manager the boundary would not move")
}

func TestApplyChangeSets_BrandNewKeysDoNotExtendMigrationTail(t *testing.T) {
	data := map[string]map[string][]byte{
		"evm": {"a": []byte("old-a"), "b": []byte("old-b")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()
	mgr, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMockMigrationIterator(copyData(data), false), 1,
	)
	require.NoError(t, err)

	require.NoError(t, mgr.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("z-new-1"), Value: []byte("new-1")},
		}},
	}}, true))
	_, ok := oldDB.get("evm", "z-new-1")
	require.False(t, ok, "brand-new keys must not be written to old DB or migration can chase a growing tail")
	val, ok := newDB.get("evm", "z-new-1")
	require.True(t, ok)
	require.Equal(t, []byte("new-1"), val)

	val, ok, err = mgr.Read("evm", []byte("z-new-1"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("new-1"), val, "unmigrated-range reads must fall back to new DB for brand-new keys")

	require.NoError(t, mgr.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("z-new-2"), Value: []byte("new-2")},
		}},
	}}, true))
	require.True(t, mgr.boundary.Equals(MigrationBoundaryComplete))
	versionBytes, ok := newDB.get(MigrationStore, MigrationVersionKey)
	require.True(t, ok, "migration should complete instead of chasing z-new-* keys forever")
	require.Len(t, versionBytes, 8)
}

func TestApplyChangeSets_ExistingUnmigratedKeyStillWritesOldDB(t *testing.T) {
	data := map[string]map[string][]byte{
		"evm": {"a": []byte("old-a"), "b": []byte("old-b"), "c": []byte("old-c")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()
	mgr, err := newTestManager(t,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMockMigrationIterator(copyData(data), false), 1,
	)
	require.NoError(t, err)

	require.NoError(t, mgr.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("c"), Value: []byte("updated-c")},
		}},
	}}, true))

	val, ok := oldDB.get("evm", "c")
	require.True(t, ok)
	require.Equal(t, []byte("updated-c"), val,
		"existing not-yet-migrated keys must remain in old DB until the iterator reaches them")
	_, ok = newDB.get("evm", "c")
	require.False(t, ok)
}

func TestBuildRoute_WriterRejectsMigrationStore(t *testing.T) {
	mgr, _, _ := inProgressManager(t)
	route, err := mgr.BuildRoute(MigrationStore)
	require.NoError(t, err)

	err = route.writer([]*proto.NamedChangeSet{
		{Name: MigrationStore, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("anything"), Value: []byte("v")},
		}}},
	}, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), MigrationStore)
}

func TestBuildRoute_GetProofReturnsNotSupported(t *testing.T) {
	mgr, _, _ := inProgressManager(t)
	route, err := mgr.BuildRoute("bank")
	require.NoError(t, err)
	require.NotNil(t, route.proofBuilder, "proof builder must be wired even when unsupported")

	proof, err := route.proofBuilder("bank", []byte("k"))
	require.Error(t, err)
	require.Nil(t, proof)
	require.Contains(t, err.Error(), "state proofs not supported")
}

// TestBuildRoute_IntegrationWithModuleRouter exercises the full
// end-to-end path: a Route built from a MigrationManager is composed
// into a ModuleRouter alongside an unrelated route, and reads/writes
// dispatched through the outer router land in the right backing DB.
func TestBuildRoute_IntegrationWithModuleRouter(t *testing.T) {
	mgr, _, mgrNewDB := inProgressManager(t)
	// Drive the manager into the post-completion state so reads and
	// writes for "bank" all flow through the new DB without any
	// migration bookkeeping interleaved into the inspected output.
	mgr.boundary = MigrationBoundaryComplete
	mgrNewDB.seed(map[string]map[string][]byte{"bank": {"k": []byte("from-mgr")}})

	mgrRoute, err := mgr.BuildRoute("bank")
	require.NoError(t, err)

	otherDB := newMockDB()
	otherDB.seed(map[string]map[string][]byte{"evm": {"k": []byte("from-other")}})
	router, err := NewModuleRouter(mgrRoute, newRoute(t, otherDB, "evm"))
	require.NoError(t, err)

	val, ok, err := router.Read("bank", []byte("k"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("from-mgr"), val)

	val, ok, err = router.Read("evm", []byte("k"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("from-other"), val)

	require.NoError(t, router.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k2"), Value: []byte("mgr-write")},
		}}},
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k2"), Value: []byte("other-write")},
		}}},
	}, true))

	val, ok = mgrNewDB.get("bank", "k2")
	require.True(t, ok)
	require.Equal(t, []byte("mgr-write"), val,
		"writes routed to the manager-backed module must reach its newDB")
	val, ok = otherDB.get("evm", "k2")
	require.True(t, ok)
	require.Equal(t, []byte("other-write"), val,
		"writes routed to the sibling module must not be redirected to the manager")
	_, ok = mgrNewDB.get("evm", "k2")
	require.False(t, ok, "sibling-module writes must not leak into the manager's newDB")
}
