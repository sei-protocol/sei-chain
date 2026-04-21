package migration

import (
	"context"
	"fmt"
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

// --- Constructor: at targetVersion ---

func TestMigrationManager_AtTargetVersion_Passthrough(t *testing.T) {
	oldDB := newMockDB()
	newDB := newMockDB()
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(7)},
	})

	mgr, err := NewMigrationManager(10,
		0, 7,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(nil, false),
	)
	require.NoError(t, err)

	require.True(t, mgr.migrationFinished)
	require.Equal(t, MigrationComplete, mgr.boundary.Status())

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

func TestMigrationManager_AtTargetVersion_NilOldHandlesAccepted(t *testing.T) {
	newDB := newMockDB()
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(1)},
	})

	mgr, err := NewMigrationManager(10,
		0, 1,
		nil, nil,
		newDB.reader(), newDB.writer(),
		nil,
	)
	require.NoError(t, err, "constructor must accept nil old-DB handles in passthrough")
	require.True(t, mgr.migrationFinished)
	require.Equal(t, uint64(1), mgr.targetVersion,
		"targetVersion must be preserved on a passthrough manager")

	// Regression guard: a constructor-passthrough manager must
	// actually take the passthrough branch in ApplyChangeSets. Without
	// versionBumped=true it would fall through to the migrating path
	// and crash dereferencing the (nil) iterator.
	changesets := []*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("a"), Value: []byte("1")},
		}}},
	}
	require.NoError(t, mgr.ApplyChangeSets(context.Background(), changesets))
	require.Len(t, newDB.writeLog, 1)
	require.Equal(t, changesets, newDB.writeLog[0],
		"passthrough must forward the caller's changesets verbatim")
}

func TestMigrationManager_NilOldHandlesRejectedWhenNotAtTargetVersion(t *testing.T) {
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
			_, err := NewMigrationManager(10,
				0, 1,
				tc.oldReader, tc.oldWriter,
				newDB.reader(), newDB.writer(),
				tc.iter,
			)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantContains)
			require.Contains(t, err.Error(), "targetVersion")
		})
	}
}

// --- Constructor: at startVersion (including chained migration) ---

// The constructor reads MigrationVersionKey from the new DB first, and
// falls back to the old DB if the new DB has no version. Either DB
// carrying startVersion is enough to start (or resume) a migration.

func TestMigrationManager_AtStartVersionInOldDB_RunsMigration(t *testing.T) {
	// Chained-migration shape: the prior migration's targetVersion (=5)
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

	mgr, err := NewMigrationManager(10,
		5, 6,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(copyData(data), false),
	)
	require.NoError(t, err)
	require.False(t, mgr.migrationFinished)

	require.NoError(t, mgr.ApplyChangeSets(context.Background(), nil))
	val, ok := newDB.get("bank", "a")
	require.True(t, ok)
	require.Equal(t, []byte("1"), val)
}

func TestMigrationManager_AtStartVersionInNewDB_RunsMigration(t *testing.T) {
	// New DB already carries startVersion (e.g. an in-place migration
	// where the caller pre-stamps the DB, or a DB that has been
	// promoted into the new slot). The constructor should accept this
	// and proceed with the migration without even consulting the old
	// DB's version key.
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "b": []byte("2")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(5)},
	})

	mgr, err := NewMigrationManager(10,
		5, 6,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(copyData(data), false),
	)
	require.NoError(t, err)
	require.False(t, mgr.migrationFinished)
	require.Equal(t, MigrationNotStarted, mgr.boundary.Status(),
		"no persisted boundary in the new DB -> start from the beginning")

	require.NoError(t, mgr.ApplyChangeSets(context.Background(), nil))
	val, ok := newDB.get("bank", "a")
	require.True(t, ok)
	require.Equal(t, []byte("1"), val)
}

func TestMigrationManager_NewDBVersionTakesPrecedenceOverOldDB(t *testing.T) {
	// If the new DB carries a valid MigrationVersionKey, the
	// constructor must trust it and skip the old-DB version check.
	// We prove that by seeding the old DB with a version that would
	// otherwise be rejected: if the old DB were consulted, the
	// constructor would return an error.
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	oldDB.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(999)}, // garbage
	})
	newDB := newMockDB()
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(5)},
	})

	mgr, err := NewMigrationManager(10,
		5, 6,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(copyData(data), false),
	)
	require.NoError(t, err, "new DB's startVersion should be authoritative, old DB not re-checked")
	require.False(t, mgr.migrationFinished)
}

func TestMigrationManager_AtStartVersionInNewDB_WithBoundary_Resumes(t *testing.T) {
	// New DB has startVersion AND a persisted mid-migration boundary.
	// The constructor must adopt that boundary rather than restart
	// from the beginning, even when the new DB's version alone would
	// be enough to start a fresh migration.
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "b": []byte("2"), "c": []byte("3")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))

	mid := NewMigrationBoundary("bank", []byte("a"))
	newDB := newMockDB()
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {
			MigrationVersionKey:  encodeVersion(5),
			MigrationBoundaryKey: mid.Serialize(),
		},
	})

	mgr, err := NewMigrationManager(10,
		5, 6,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(copyData(data), false),
	)
	require.NoError(t, err)
	require.False(t, mgr.migrationFinished)
	require.True(t, mgr.boundary.Equals(mid), "persisted boundary must be adopted on startup")
}

func TestMigrationManager_AtStartVersionAbsent_RunsMigration(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()

	mgr, err := NewMigrationManager(10,
		0, 1,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(copyData(data), false),
	)
	require.NoError(t, err)
	require.False(t, mgr.migrationFinished)
}

func TestMigrationManager_UnexpectedVersionInOldDB_Errors(t *testing.T) {
	// New DB has no version; we fall back to the old DB, whose version
	// must equal startVersion. Any other value (including the
	// migration's targetVersion) is a hard error: by design only the
	// new DB ever reaches targetVersion.
	oldDB := newMockDB()
	oldDB.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(42)},
	})
	newDB := newMockDB()

	_, err := NewMigrationManager(10,
		5, 6,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(nil, false),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected migration version in old DB")
	require.Contains(t, err.Error(), "42", "error should name the actual (unexpected) version")
	require.Contains(t, err.Error(), "5", "error should name the expected startVersion")
}

func TestMigrationManager_UnexpectedVersionInNewDB_Errors(t *testing.T) {
	// New DB carries a version that is neither startVersion nor
	// targetVersion — flag it. Use a spread where start, target, and
	// actual are all distinct so we can assert each number is named.
	oldDB := newMockDB()
	newDB := newMockDB()
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(99)},
	})

	_, err := NewMigrationManager(10,
		5, 10,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(nil, false),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected migration version in new DB")
	require.Contains(t, err.Error(), "99", "error should name the actual (unexpected) version")
	require.Contains(t, err.Error(), "5", "error should name the expected startVersion")
	require.Contains(t, err.Error(), "10", "error should name the expected targetVersion")
}

func TestMigrationManager_VersionedAgainstGarbageInOldDB_Unchecked(t *testing.T) {
	// When the new DB already reports targetVersion we enter
	// passthrough without reading the old DB's version at all, even if
	// the old DB is still around with some unexpected value on it.
	oldDB := newMockDB()
	oldDB.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(999)},
	})
	newDB := newMockDB()
	newDB.seed(map[string]map[string][]byte{
		MigrationStore: {MigrationVersionKey: encodeVersion(6)},
	})

	mgr, err := NewMigrationManager(10,
		5, 6,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(nil, false),
	)
	require.NoError(t, err, "passthrough path must not consult the old DB's version")
	require.True(t, mgr.migrationFinished)
}

func TestMigrationManager_StartVersionMustBeLessThanTarget(t *testing.T) {
	_, err := NewMigrationManager(10,
		5, 5,
		nil, nil, newMockDB().reader(), newMockDB().writer(), nil,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "startVersion")
	require.Contains(t, err.Error(), "targetVersion")
}

// --- Bump block ---

func TestMigrationManager_FinalCallWritesVersionAtomically(t *testing.T) {
	// Two keys + batch size 2: the single ApplyChangeSets call both
	// migrates everything and finalizes (writes targetVersion, deletes
	// boundary) in one atomic new-DB changeset.
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "b": []byte("2")},
	}

	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()

	mgr, err := NewMigrationManager(2,
		0, 1,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(copyData(data), false),
	)
	require.NoError(t, err)
	require.False(t, mgr.migrationFinished)

	// Caller hands in a real change alongside the finalizing batch.
	callerCS := []*proto.NamedChangeSet{
		{Name: "auth", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("x"), Value: []byte("caller-x")},
		}}},
	}
	require.NoError(t, mgr.ApplyChangeSets(context.Background(), callerCS))

	// Exactly one write to the new DB, atomic, combining migrated
	// values + caller pairs + the MigrationStore maintenance entry.
	require.Len(t, newDB.writeLog, 1, "final call must write the new DB exactly once")
	finalCS := newDB.writeLog[0]

	// auth and bank first (sorted), then MigrationStore appended.
	require.Len(t, finalCS, 3)
	require.Equal(t, "auth", finalCS[0].Name)
	require.Equal(t, callerCS[0].Changeset.Pairs, finalCS[0].Changeset.Pairs)
	require.Equal(t, "bank", finalCS[1].Name)
	require.Equal(t, MigrationStore, finalCS[2].Name)

	// bank entries are the migrated values, sorted by key.
	bankPairs := finalCS[1].Changeset.Pairs
	require.Len(t, bankPairs, 2)
	require.Equal(t, []byte("a"), bankPairs[0].Key)
	require.Equal(t, []byte("1"), bankPairs[0].Value)
	require.Equal(t, []byte("b"), bankPairs[1].Key)
	require.Equal(t, []byte("2"), bankPairs[1].Value)

	// MigrationStore entry: one version write + one boundary delete.
	msPairs := finalCS[2].Changeset.Pairs
	require.Len(t, msPairs, 2)

	pairByKey := make(map[string]*proto.KVPair, len(msPairs))
	for _, p := range msPairs {
		pairByKey[string(p.Key)] = p
	}

	verPair, ok := pairByKey[MigrationVersionKey]
	require.True(t, ok, "MigrationVersionKey write missing from final call")
	require.False(t, verPair.Delete)
	require.Equal(t, encodeVersion(1), verPair.Value)

	bPair, ok := pairByKey[MigrationBoundaryKey]
	require.True(t, ok, "MigrationBoundaryKey delete missing from final call")
	require.True(t, bPair.Delete)

	// Post-state: the new DB's MigrationStore now contains only the
	// version key, and never saw a persisted boundary.
	v, ok := newDB.get(MigrationStore, MigrationVersionKey)
	require.True(t, ok)
	require.Equal(t, encodeVersion(1), v)
	_, ok = newDB.get(MigrationStore, MigrationBoundaryKey)
	require.False(t, ok, "MigrationBoundaryKey should be absent after final call")

	// Caller's write landed.
	authVal, ok := newDB.get("auth", "x")
	require.True(t, ok)
	require.Equal(t, []byte("caller-x"), authVal)

	// Old DB writer was invoked once with the migration deletes for
	// the keys migrated on this final call. Callers that want to
	// preserve the old DB are expected to tear down the handle
	// themselves rather than rely on the manager to skip these writes.
	require.Len(t, oldDB.writeLog, 1, "final call still fans migration deletes to old DB")
	oldCS := oldDB.writeLog[0]
	require.Len(t, oldCS, 1)
	require.Equal(t, "bank", oldCS[0].Name)
	require.Len(t, oldCS[0].Changeset.Pairs, 2)
	for _, p := range oldCS[0].Changeset.Pairs {
		require.True(t, p.Delete, "old-DB pair %q on final call must be a delete", p.Key)
	}

	// Manager is now in passthrough.
	require.True(t, mgr.migrationFinished)
	require.Equal(t, MigrationComplete, mgr.boundary.Status())
}

func TestMigrationManager_FinalCallSubsequentCallsPassthrough(t *testing.T) {
	data := map[string]map[string][]byte{"bank": {"a": []byte("1")}}

	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()

	mgr, err := NewMigrationManager(10,
		0, 1,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(copyData(data), false),
	)
	require.NoError(t, err)

	// Single call finishes migration and bumps the version.
	require.NoError(t, mgr.ApplyChangeSets(context.Background(), nil))
	require.True(t, mgr.migrationFinished)

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
}
