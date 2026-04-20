package migration

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// Crash-recovery tests exercise MigrationManager's WAL replay by simulating
// a partial write: run a successful ApplyChangeSets, then roll one or both
// mock DBs back to their pre-batch state, then construct a fresh
// MigrationManager against the same WAL directory. The constructor should
// notice the lagging DB(s) and replay the WAL payload to bring them back
// in sync without any loss of forward progress.
//
// Each test uses a shared walDir so the first manager's durable WAL entry
// is visible to the second manager. mock DBs are in-memory so a rollback
// is just `oldDB.data = <snapshot>`.

// applyOneBatch runs mgr.ApplyChangeSets(nil) and returns the pre-batch
// deep-copied state of both mock DBs, so the caller can selectively roll
// either side back to simulate a crash.
func applyOneBatch(
	t *testing.T,
	mgr *MigrationManager,
	oldDB, newDB *mockDB,
) (oldSnapshot, newSnapshot map[string]map[string][]byte) {
	t.Helper()
	oldSnapshot = copyData(oldDB.data)
	newSnapshot = copyData(newDB.data)
	require.NoError(t, mgr.ApplyChangeSets(context.Background(), nil))
	return oldSnapshot, newSnapshot
}

// writeBatchCounter pokes a batch counter into the given mock DB's
// MigrationStore. Used to construct corrupt states by hand.
func writeBatchCounter(db *mockDB, key string, v uint64) {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	if db.data[MigrationStore] == nil {
		db.data[MigrationStore] = make(map[string][]byte)
	}
	db.data[MigrationStore][key] = b
}

func readBatchCounter(t *testing.T, db *mockDB, key string) uint64 {
	t.Helper()
	b, ok := db.get(MigrationStore, key)
	require.True(t, ok, "batch counter %q missing", key)
	require.Len(t, b, 8)
	return binary.BigEndian.Uint64(b)
}

// --- Happy path: nothing to recover ---

func TestRecovery_CleanReopenNoReplay(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "b": []byte("2")},
	}
	walDir := t.TempDir()

	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()

	iter1 := NewMapMigrationIterator(copyData(data), false)
	mgr1, err := NewMigrationManager(walDir, 2,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter1,
	)
	require.NoError(t, err)

	require.NoError(t, mgr1.ApplyChangeSets(context.Background(), nil))
	require.Equal(t, uint64(1), readBatchCounter(t, oldDB, OldDBBatchIDKey))
	require.Equal(t, uint64(1), readBatchCounter(t, newDB, NewDBBatchIDKey))

	// Reopen. Both DBs are already at WAL latest -> no replay.
	oldDB.writeLog = nil
	newDB.writeLog = nil

	iter2 := NewMapMigrationIterator(copyData(data), false)
	mgr2, err := NewMigrationManager(walDir, 2,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter2,
	)
	require.NoError(t, err)
	require.True(t, mgr2.boundary.Equals(mgr1.boundary),
		"reopened manager should adopt the persisted boundary")

	// Neither DB should have been touched by the constructor.
	require.Empty(t, oldDB.writeLog, "no replay should have hit the old DB")
	require.Empty(t, newDB.writeLog, "no replay should have hit the new DB")
}

// --- Old DB lost its write ---

func TestRecovery_OldDBLagsOneBatch(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank":    {"a": []byte("1"), "b": []byte("2")},
		"staking": {"x": []byte("3"), "y": []byte("4")},
	}
	walDir := t.TempDir()

	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()

	iter1 := NewMapMigrationIterator(copyData(data), false)
	mgr1, err := NewMigrationManager(walDir, 2,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter1,
	)
	require.NoError(t, err)

	// Apply one batch successfully, then roll the old DB back to its
	// pre-batch state. New DB keeps the batch. WAL keeps the record.
	oldSnapshot, _ := applyOneBatch(t, mgr1, oldDB, newDB)
	oldDB.data = oldSnapshot
	oldDB.writeLog = nil
	newDB.writeLog = nil

	// Sanity: old DB really did lose the batch.
	_, ok := oldDB.get(MigrationStore, OldDBBatchIDKey)
	require.False(t, ok, "old DB rollback should have dropped the batch counter")

	// Reopen. Constructor should replay the WAL record to the old DB.
	iter2 := NewMapMigrationIterator(copyData(data), false)
	mgr2, err := NewMigrationManager(walDir, 2,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter2,
	)
	require.NoError(t, err)

	// Only the old DB should have been replayed.
	require.Len(t, oldDB.writeLog, 1, "old DB should have been replayed exactly once")
	require.Empty(t, newDB.writeLog, "new DB should not have been replayed")

	// Old DB's batch counter is back in sync, and the migration deletes
	// have been re-applied: bank/a and bank/b should be gone.
	require.Equal(t, uint64(1), readBatchCounter(t, oldDB, OldDBBatchIDKey))
	_, ok = oldDB.get("bank", "a")
	require.False(t, ok, "bank/a should be deleted from old DB after replay")
	_, ok = oldDB.get("bank", "b")
	require.False(t, ok, "bank/b should be deleted from old DB after replay")

	// Boundary matches what the original manager had.
	require.True(t, mgr2.boundary.Equals(mgr1.boundary))

	// Manager can continue without re-migrating already-migrated keys.
	newDB.writeLog = nil
	oldDB.writeLog = nil
	require.NoError(t, mgr2.ApplyChangeSets(context.Background(), nil))

	// That second batch should pick up from staking/x onward, not replay
	// bank/a or bank/b.
	for _, batch := range newDB.writeLog {
		for _, cs := range batch {
			if cs.Name != "bank" {
				continue
			}
			for _, pair := range cs.Changeset.Pairs {
				require.NotContains(t, []string{"a", "b"}, string(pair.Key),
					"already-migrated bank key should not reappear")
			}
		}
	}
}

// --- New DB lost its write ---

func TestRecovery_NewDBLagsOneBatch(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "b": []byte("2"), "c": []byte("3")},
	}
	walDir := t.TempDir()

	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()

	iter1 := NewMapMigrationIterator(copyData(data), false)
	mgr1, err := NewMigrationManager(walDir, 2,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter1,
	)
	require.NoError(t, err)

	// Apply one batch, then lose the new DB's effects.
	_, newSnapshot := applyOneBatch(t, mgr1, oldDB, newDB)
	newDB.data = newSnapshot
	oldDB.writeLog = nil
	newDB.writeLog = nil

	// Sanity: new DB has no batch counter or migrated keys.
	_, ok := newDB.get(MigrationStore, NewDBBatchIDKey)
	require.False(t, ok)
	_, ok = newDB.get("bank", "a")
	require.False(t, ok)

	// Reopen. Constructor should replay the WAL record to the new DB.
	iter2 := NewMapMigrationIterator(copyData(data), false)
	mgr2, err := NewMigrationManager(walDir, 2,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter2,
	)
	require.NoError(t, err)

	require.Empty(t, oldDB.writeLog, "old DB should not have been replayed")
	require.Len(t, newDB.writeLog, 1, "new DB should have been replayed exactly once")

	// New DB's batch counter and migrated pairs land again.
	require.Equal(t, uint64(1), readBatchCounter(t, newDB, NewDBBatchIDKey))
	val, ok := newDB.get("bank", "a")
	require.True(t, ok)
	require.Equal(t, []byte("1"), val)
	val, ok = newDB.get("bank", "b")
	require.True(t, ok)
	require.Equal(t, []byte("2"), val)

	// Boundary recovered from the replayed write, not the pre-crash read.
	require.True(t, mgr2.boundary.Equals(mgr1.boundary))
}

// --- Both DBs lost their write ---

func TestRecovery_BothDBsLagOneBatch(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "b": []byte("2")},
	}
	walDir := t.TempDir()

	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()

	iter1 := NewMapMigrationIterator(copyData(data), false)
	mgr1, err := NewMigrationManager(walDir, 2,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter1,
	)
	require.NoError(t, err)

	oldSnapshot, newSnapshot := applyOneBatch(t, mgr1, oldDB, newDB)
	oldDB.data = oldSnapshot
	newDB.data = newSnapshot
	oldDB.writeLog = nil
	newDB.writeLog = nil

	iter2 := NewMapMigrationIterator(copyData(data), false)
	mgr2, err := NewMigrationManager(walDir, 2,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter2,
	)
	require.NoError(t, err)

	// Both DBs replayed once.
	require.Len(t, oldDB.writeLog, 1)
	require.Len(t, newDB.writeLog, 1)

	// Post-replay state: matches what the first manager had persisted.
	require.Equal(t, uint64(1), readBatchCounter(t, oldDB, OldDBBatchIDKey))
	require.Equal(t, uint64(1), readBatchCounter(t, newDB, NewDBBatchIDKey))
	_, ok := oldDB.get("bank", "a")
	require.False(t, ok)
	val, ok := newDB.get("bank", "a")
	require.True(t, ok)
	require.Equal(t, []byte("1"), val)
	require.True(t, mgr2.boundary.Equals(mgr1.boundary))
}

// --- Incoming change sets are replayed, not just the migration itself ---

func TestRecovery_ReplayIncludesIncomingWrites(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1")},
	}
	walDir := t.TempDir()

	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()

	iter1 := NewMapMigrationIterator(copyData(data), false)
	mgr1, err := NewMigrationManager(walDir, 10,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter1,
	)
	require.NoError(t, err)

	// Snapshot before the incoming batch.
	newSnapshot := copyData(newDB.data)
	oldSnapshot := copyData(oldDB.data)

	// The batch migrates bank/a AND writes a fresh pair bank/z (not in
	// the iterator, so it's purely an incoming write against an
	// unmigrated key). bank/z will be routed to the old DB.
	changesets := []*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("z"), Value: []byte("from-caller")},
		}}},
	}
	require.NoError(t, mgr1.ApplyChangeSets(context.Background(), changesets))

	// Roll both DBs back.
	oldDB.data = oldSnapshot
	newDB.data = newSnapshot
	oldDB.writeLog = nil
	newDB.writeLog = nil

	// Reopen. Replay should reproduce the same bank/z write in the old
	// DB alongside the migration delete for bank/a.
	iter2 := NewMapMigrationIterator(copyData(data), false)
	_, err = NewMigrationManager(walDir, 10,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter2,
	)
	require.NoError(t, err)

	// bank/z was routed to old DB as an incoming write.
	val, ok := oldDB.get("bank", "z")
	require.True(t, ok, "bank/z should be in old DB after replay")
	require.Equal(t, []byte("from-caller"), val)

	// bank/a was migrated: deleted from old DB, present in new DB.
	_, ok = oldDB.get("bank", "a")
	require.False(t, ok)
	val, ok = newDB.get("bank", "a")
	require.True(t, ok)
	require.Equal(t, []byte("1"), val)
}

// --- Multiple successive batches each recover cleanly ---

func TestRecovery_RepeatedCrashesThroughFullMigration(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank":    {"a": []byte("1"), "b": []byte("2")},
		"staking": {"x": []byte("3"), "y": []byte("4")},
	}
	walDir := t.TempDir()

	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()

	// Drive migration to completion, but reopen the manager after every
	// batch, selectively rolling back one side to force recovery.
	for round := 0; ; round++ {
		iter := NewMapMigrationIterator(copyData(data), false)
		mgr, err := NewMigrationManager(walDir, 2,
			oldDB.reader(), oldDB.writer(),
			newDB.reader(), newDB.writer(),
			iter,
		)
		require.NoError(t, err, "round %d", round)

		if mgr.boundary.Status() == MigrationComplete {
			break
		}

		oldSnapshot, newSnapshot := applyOneBatch(t, mgr, oldDB, newDB)
		switch round % 3 {
		case 0:
			// Lose old DB.
			oldDB.data = oldSnapshot
		case 1:
			// Lose new DB.
			newDB.data = newSnapshot
		case 2:
			// Both survived. Clean reopen on the next iteration.
		}
	}

	// Final state: all keys in new DB, none in old DB, both counters match.
	for store, kvs := range data {
		for k, v := range kvs {
			val, ok := newDB.get(store, k)
			require.True(t, ok, "%s/%s missing from new DB", store, k)
			require.Equal(t, v, val)

			_, ok = oldDB.get(store, k)
			require.False(t, ok, "%s/%s should be deleted from old DB", store, k)
		}
	}
	require.Equal(t, readBatchCounter(t, oldDB, OldDBBatchIDKey),
		readBatchCounter(t, newDB, NewDBBatchIDKey),
		"both DB counters should match after full recovery run")
}

// --- Corruption: DB counter too far behind WAL ---

func TestRecovery_RejectsOldDBTooFarBehind(t *testing.T) {
	walDir := t.TempDir()
	oldDB := newMockDB()
	newDB := newMockDB()

	// Bring the WAL to batch 1, both DBs to batch 1.
	iter := NewMapMigrationIterator(map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "b": []byte("2")},
	}, false)
	oldDB.seed(map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "b": []byte("2")},
	})
	mgr, err := NewMigrationManager(walDir, 1,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter,
	)
	require.NoError(t, err)

	// Run batch 1 and batch 2; only the WAL's latest (batch 2) survives.
	require.NoError(t, mgr.ApplyChangeSets(context.Background(), nil))
	require.NoError(t, mgr.ApplyChangeSets(context.Background(), nil))
	require.Equal(t, uint64(2), readBatchCounter(t, oldDB, OldDBBatchIDKey))

	// Corrupt: force the old DB counter back to 0 (two behind the WAL
	// latest of 2). Recovery can only replay the single latest record,
	// so a two-batch gap is unrecoverable and must be rejected.
	writeBatchCounter(oldDB, OldDBBatchIDKey, 0)

	iter2 := NewMapMigrationIterator(nil, false)
	_, err = NewMigrationManager(walDir, 1,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter2,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "old DB")
	require.Contains(t, err.Error(), "corruption")
}

// --- Corruption: DB counter ahead of WAL ---

func TestRecovery_RejectsNewDBAheadOfWAL(t *testing.T) {
	walDir := t.TempDir()
	oldDB := newMockDB()
	newDB := newMockDB()

	iter := NewMapMigrationIterator(map[string]map[string][]byte{
		"bank": {"a": []byte("1")},
	}, false)
	oldDB.seed(map[string]map[string][]byte{
		"bank": {"a": []byte("1")},
	})
	mgr, err := NewMigrationManager(walDir, 1,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter,
	)
	require.NoError(t, err)
	require.NoError(t, mgr.ApplyChangeSets(context.Background(), nil))

	// Corrupt: push the new DB counter to a value the WAL could never
	// have sanctioned.
	writeBatchCounter(newDB, NewDBBatchIDKey, 99)

	iter2 := NewMapMigrationIterator(nil, false)
	_, err = NewMigrationManager(walDir, 1,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter2,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "new DB")
	require.Contains(t, err.Error(), "corruption")
}

// --- Corruption: non-8-byte counter ---

func TestRecovery_RejectsMalformedCounter(t *testing.T) {
	walDir := t.TempDir()
	oldDB := newMockDB()
	newDB := newMockDB()

	oldDB.data[MigrationStore] = map[string][]byte{
		OldDBBatchIDKey: {0x01, 0x02, 0x03}, // not 8 bytes
	}

	iter := NewMapMigrationIterator(nil, false)
	_, err := NewMigrationManager(walDir, 1,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		iter,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "batch ID")
}
