package migration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// --- IsAtVersion ---

func TestIsAtVersion_MatchingKey(t *testing.T) {
	db := newMockDB()
	db.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(7)},
	})
	ok, err := IsAtVersion(db.reader(), 7)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestIsAtVersion_NonMatchingKey(t *testing.T) {
	db := newMockDB()
	db.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(7)},
	})
	ok, err := IsAtVersion(db.reader(), 8)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestIsAtVersion_AbsentKeyEqualsZero(t *testing.T) {
	db := newMockDB()

	ok, err := IsAtVersion(db.reader(), 0)
	require.NoError(t, err)
	require.True(t, ok, "absent version key should match 0")

	ok, err = IsAtVersion(db.reader(), 1)
	require.NoError(t, err)
	require.False(t, ok, "absent version key should not match any non-zero version")
}

func TestIsAtVersion_MalformedValueErrors(t *testing.T) {
	db := newMockDB()
	db.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: []byte{0x01, 0x02, 0x03}},
	})
	_, err := IsAtVersion(db.reader(), 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "8-byte")
}

func TestIsAtVersion_ReaderErrorPropagates(t *testing.T) {
	want := fmt.Errorf("disk on fire")
	_, err := IsAtVersion(failReader(want), 0)
	require.ErrorIs(t, err, want)
}

// --- Constructor: at destVersion ---

// countingFinalize returns a finalizer that records how many times it was
// invoked.
func countingFinalize() (func(), *int) {
	var calls int
	return func() { calls++ }, &calls
}

// seedWALDir creates walDir and drops a non-empty sentinel file inside so
// tests can verify that the manager actually removed the directory.
func seedWALDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	walDir := filepath.Join(dir, "wal")
	require.NoError(t, os.MkdirAll(walDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(walDir, "sentinel"), []byte("x"), 0o644))
	return walDir
}

func TestMigrationManager_AtDestVersion_PassthroughAndCleanup(t *testing.T) {
	walDir := seedWALDir(t)

	oldDB := newMockDB()
	newDB := newMockDB()
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(7)},
	})

	fin, calls := countingFinalize()
	mgr, err := NewMigrationManager(walDir, 10,
		0, 7, fin,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(nil, false),
	)
	require.NoError(t, err)

	require.True(t, mgr.versionBumped)
	require.Equal(t, MigrationComplete, mgr.boundary.Status())
	require.Equal(t, 1, *calls, "finalizeMigration should fire on constructor passthrough")

	// WAL dir is gone.
	_, statErr := os.Stat(walDir)
	require.True(t, os.IsNotExist(statErr), "WAL dir should be removed; err=%v", statErr)

	// ApplyChangeSets forwards caller's writes to new DB only, with no
	// MigrationStore injection and no old-DB writes.
	changesets := []*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("a"), Value: []byte("1")},
		}}},
	}
	require.NoError(t, mgr.ApplyChangeSets(context.Background(), changesets))
	require.Empty(t, oldDB.writeLog)
	require.Len(t, newDB.writeLog, 1)
	require.Equal(t, changesets, newDB.writeLog[0])
}

func TestMigrationManager_AtDestVersion_NilOldHandlesAccepted(t *testing.T) {
	walDir := seedWALDir(t)

	newDB := newMockDB()
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(1)},
	})

	fin, calls := countingFinalize()
	mgr, err := NewMigrationManager(walDir, 10,
		0, 1, fin,
		nil, nil,
		newDB.reader(), newDB.writer(),
		nil,
	)
	require.NoError(t, err, "constructor must accept nil old-DB handles in passthrough")
	require.True(t, mgr.versionBumped)
	require.Equal(t, 1, *calls)
}

func TestMigrationManager_NilOldHandlesRejectedWhenNotAtDestVersion(t *testing.T) {
	walDir := t.TempDir()
	newDB := newMockDB()

	cases := []struct {
		name         string
		oldReader    DBReader
		oldWriter    DBWriter
		iter         MigrationIterator
		wantContains string
	}{
		{"nil oldDBReader", nil, newMockDB().writer(), NewMapMigrationIterator(nil, false), "oldDBReader"},
		{"nil oldDBWriter", newMockDB().reader(), nil, NewMapMigrationIterator(nil, false), "oldDBWriter"},
		{"nil iterator", newMockDB().reader(), newMockDB().writer(), nil, "iterator"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewMigrationManager(walDir, 10,
				0, 1, noopFinalize,
				tc.oldReader, tc.oldWriter,
				newDB.reader(), newDB.writer(),
				tc.iter,
			)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantContains)
			require.Contains(t, err.Error(), "destVersion")
		})
	}
}

// --- Constructor: at startVersion (including chained migration) ---

func TestMigrationManager_AtStartVersionInOldDB_RunsMigration(t *testing.T) {
	// Chained-migration shape: the prior migration's destVersion (=5)
	// lives in the old DB. This manager transitions 5 -> 6.
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "b": []byte("2")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	oldDB.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(5)},
	})
	newDB := newMockDB()

	mgr, err := NewMigrationManager(t.TempDir(), 10,
		5, 6, noopFinalize,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(copyData(data), false),
	)
	require.NoError(t, err)
	require.False(t, mgr.versionBumped)

	require.NoError(t, mgr.ApplyChangeSets(context.Background(), nil))
	val, ok := newDB.get("bank", "a")
	require.True(t, ok)
	require.Equal(t, []byte("1"), val)
}

func TestMigrationManager_AtStartVersionAbsent_RunsMigration(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()

	mgr, err := NewMigrationManager(t.TempDir(), 10,
		0, 1, noopFinalize,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(copyData(data), false),
	)
	require.NoError(t, err)
	require.False(t, mgr.versionBumped)
}

func TestMigrationManager_UnexpectedVersion_Errors(t *testing.T) {
	oldDB := newMockDB()
	oldDB.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(42)},
	})
	newDB := newMockDB()

	_, err := NewMigrationManager(t.TempDir(), 10,
		5, 6, noopFinalize,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(nil, false),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected migration version")
	require.Contains(t, err.Error(), "42")
	require.Contains(t, err.Error(), "5")
	require.Contains(t, err.Error(), "6")
}

func TestMigrationManager_UnexpectedVersionInNewDB_Errors(t *testing.T) {
	oldDB := newMockDB()
	newDB := newMockDB()
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(99)},
	})

	_, err := NewMigrationManager(t.TempDir(), 10,
		0, 1, noopFinalize,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(nil, false),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected migration version in new DB")
	require.Contains(t, err.Error(), "99")
	require.Contains(t, err.Error(), "1")
}

func TestMigrationManager_StartVersionMustBeLessThanDest(t *testing.T) {
	_, err := NewMigrationManager(t.TempDir(), 10,
		5, 5, noopFinalize,
		nil, nil, newMockDB().reader(), newMockDB().writer(), nil,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "startVersion")
	require.Contains(t, err.Error(), "destVersion")
}

// --- Bump block ---

func TestMigrationManager_BumpBlockWritesVersionAndCleansUp(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "b": []byte("2")},
	}
	walDir := t.TempDir()

	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()

	fin, calls := countingFinalize()
	mgr, err := NewMigrationManager(walDir, 10,
		0, 1, fin,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(copyData(data), false),
	)
	require.NoError(t, err)

	// Drive to MigrationComplete in the migrating path. After this, the
	// next ApplyChangeSets is the bump block.
	require.NoError(t, mgr.ApplyChangeSets(context.Background(), nil)) // migrates a, b
	require.NoError(t, mgr.ApplyChangeSets(context.Background(), nil)) // empty batch -> boundary=Complete
	require.Equal(t, MigrationComplete, mgr.boundary.Status())
	require.False(t, mgr.versionBumped)

	// Sanity: MigrationBoundaryKey definitely exists in the new DB by
	// this point, so we can later verify the bump block's delete fires.
	_, ok := newDB.get(MigrationStore, MigrationBoundaryKey)
	require.True(t, ok)

	// Sanity: finalizer not yet called.
	require.Equal(t, 0, *calls)

	// Capture old-DB log state so we can verify the bump block doesn't
	// touch the old DB.
	oldLogLenBefore := len(oldDB.writeLog)
	newDB.writeLog = nil

	// Bump block: caller hands in a real change.
	callerCS := []*proto.NamedChangeSet{
		{Name: "auth", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("x"), Value: []byte("caller-x")},
		}}},
	}
	require.NoError(t, mgr.ApplyChangeSets(context.Background(), callerCS))

	// Exactly one write to the new DB, atomic, combining caller pairs +
	// the MigrationStore maintenance entry.
	require.Len(t, newDB.writeLog, 1, "bump block must write the new DB exactly once")
	bumpCS := newDB.writeLog[0]

	// auth first (sorted), then MigrationStore appended.
	require.Len(t, bumpCS, 2)
	require.Equal(t, "auth", bumpCS[0].Name)
	require.Equal(t, callerCS[0].Changeset.Pairs, bumpCS[0].Changeset.Pairs)
	require.Equal(t, MigrationStore, bumpCS[1].Name)

	// MigrationStore entry: one version write + one boundary delete.
	msPairs := bumpCS[1].Changeset.Pairs
	require.Len(t, msPairs, 2)

	pairByKey := make(map[string]*proto.KVPair, len(msPairs))
	for _, p := range msPairs {
		pairByKey[string(p.Key)] = p
	}

	verPair, ok := pairByKey[MigrationVersionKey]
	require.True(t, ok, "MigrationVersionKey write missing from bump block")
	require.False(t, verPair.Delete)
	require.Equal(t, encodeVersion(1), verPair.Value)

	bPair, ok := pairByKey[MigrationBoundaryKey]
	require.True(t, ok, "MigrationBoundaryKey delete missing from bump block")
	require.True(t, bPair.Delete)

	// Post-state: the new DB's MigrationStore now contains only the
	// version key.
	v, ok := newDB.get(MigrationStore, MigrationVersionKey)
	require.True(t, ok)
	require.Equal(t, encodeVersion(1), v)
	_, ok = newDB.get(MigrationStore, MigrationBoundaryKey)
	require.False(t, ok, "MigrationBoundaryKey should be gone after bump")

	// Caller's write landed.
	authVal, ok := newDB.get("auth", "x")
	require.True(t, ok)
	require.Equal(t, []byte("caller-x"), authVal)

	// Old DB writer was not invoked by the bump block. The finalizer is
	// expected to drop the old DB entirely.
	require.Equal(t, oldLogLenBefore, len(oldDB.writeLog),
		"bump block must not write to old DB")

	// WAL directory removed.
	_, statErr := os.Stat(walDir)
	require.True(t, os.IsNotExist(statErr), "WAL dir should be removed; err=%v", statErr)

	// Finalizer fired exactly once.
	require.Equal(t, 1, *calls)

	// Manager is now in passthrough.
	require.True(t, mgr.versionBumped)
}

func TestMigrationManager_BumpBlockSubsequentCallsPassthrough(t *testing.T) {
	data := map[string]map[string][]byte{"bank": {"a": []byte("1")}}
	walDir := t.TempDir()

	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()

	fin, calls := countingFinalize()
	mgr, err := NewMigrationManager(walDir, 10,
		0, 1, fin,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(copyData(data), false),
	)
	require.NoError(t, err)

	// Drive to complete + bump.
	require.NoError(t, mgr.ApplyChangeSets(context.Background(), nil)) // migrate a
	require.NoError(t, mgr.ApplyChangeSets(context.Background(), nil)) // boundary -> Complete
	require.NoError(t, mgr.ApplyChangeSets(context.Background(), nil)) // bump block
	require.True(t, mgr.versionBumped)
	require.Equal(t, 1, *calls)

	// Further calls: pure passthrough.
	newDB.writeLog = nil
	oldLogLenBefore := len(oldDB.writeLog)
	for i := 0; i < 3; i++ {
		cs := []*proto.NamedChangeSet{
			{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte(fmt.Sprintf("k%d", i)), Value: []byte("v")},
			}}},
		}
		require.NoError(t, mgr.ApplyChangeSets(context.Background(), cs))
		require.Equal(t, cs, newDB.writeLog[len(newDB.writeLog)-1],
			"passthrough should forward the caller's changesets verbatim")
	}
	require.Equal(t, oldLogLenBefore, len(oldDB.writeLog),
		"post-bump calls must not touch old DB")
	require.Equal(t, 1, *calls, "finalizer must not re-fire on subsequent passthrough calls")
}

// --- Constructor retries cleanup after a bump-block crash ---

func TestMigrationManager_ConstructorRetriesCleanupAfterBumpCrash(t *testing.T) {
	// Simulate a crash right after the bump-block's atomic write but
	// before cleanup finished: the new DB reports destVersion, the WAL
	// dir still exists with stale contents, and finalizeMigration has
	// not yet been called.
	walDir := seedWALDir(t)

	newDB := newMockDB()
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(1)},
	})

	fin, calls := countingFinalize()
	mgr, err := NewMigrationManager(walDir, 10,
		0, 1, fin,
		nil, nil,
		newDB.reader(), newDB.writer(),
		nil,
	)
	require.NoError(t, err)
	require.True(t, mgr.versionBumped)

	_, statErr := os.Stat(walDir)
	require.True(t, os.IsNotExist(statErr), "WAL dir should be removed on retry")
	require.Equal(t, 1, *calls)
}

func TestMigrationManager_FinalizeCalledOnEveryBootWhenAtDestVersion(t *testing.T) {
	newDB := newMockDB()
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(1)},
	})

	fin, calls := countingFinalize()

	// Boot 1.
	_, err := NewMigrationManager(t.TempDir(), 10,
		0, 1, fin,
		nil, nil,
		newDB.reader(), newDB.writer(),
		nil,
	)
	require.NoError(t, err)

	// Boot 2.
	_, err = NewMigrationManager(t.TempDir(), 10,
		0, 1, fin,
		nil, nil,
		newDB.reader(), newDB.writer(),
		nil,
	)
	require.NoError(t, err)

	require.Equal(t, 2, *calls,
		"finalizer should fire on every boot while the DB reports destVersion")
}

// --- WAL delete failure is non-fatal ---

// TestMigrationManager_WALDeleteFailureIsNonFatal makes a WAL directory
// that RemoveAll cannot delete by stripping write permission from its
// parent. The constructor and bump block both wrap os.RemoveAll in a
// logged warning rather than returning an error; we verify the manager
// still comes back ready.
func TestMigrationManager_WALDeleteFailureIsNonFatal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-based RemoveAll failure trick does not apply on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can delete directories regardless of parent permissions")
	}

	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	require.NoError(t, os.Mkdir(parent, 0o755))
	walDir := filepath.Join(parent, "wal")
	require.NoError(t, os.Mkdir(walDir, 0o755))

	// Strip write permission from the parent so os.RemoveAll cannot
	// unlink walDir itself. Defer restoring it so the test's TempDir
	// cleanup works.
	require.NoError(t, os.Chmod(parent, 0o555))
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	newDB := newMockDB()
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(1)},
	})

	fin, calls := countingFinalize()
	mgr, err := NewMigrationManager(walDir, 10,
		0, 1, fin,
		nil, nil,
		newDB.reader(), newDB.writer(),
		nil,
	)
	require.NoError(t, err, "WAL-delete failure must not fail the constructor")
	require.True(t, mgr.versionBumped)
	require.Equal(t, 1, *calls, "finalizer still fires on constructor path")

	// ApplyChangeSets still works (passthrough forwards to new DB).
	changesets := []*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("a"), Value: []byte("1")},
		}}},
	}
	require.NoError(t, mgr.ApplyChangeSets(context.Background(), changesets))
}
