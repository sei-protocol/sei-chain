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

	require.True(t, mgr.versionBumped)
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
	require.True(t, mgr.versionBumped)
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

	mgr, err := NewMigrationManager(10,
		0, 1,
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

	_, err := NewMigrationManager(10,
		5, 6,
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

	_, err := NewMigrationManager(10,
		0, 1,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(nil, false),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected migration version in new DB")
	require.Contains(t, err.Error(), "99")
	require.Contains(t, err.Error(), "1")
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

func TestMigrationManager_BumpBlockWritesVersionAtomically(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "b": []byte("2")},
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

	// Old DB writer was not invoked by the bump block. Outer context is
	// expected to drop the old DB entirely.
	require.Equal(t, oldLogLenBefore, len(oldDB.writeLog),
		"bump block must not write to old DB")

	// Manager is now in passthrough.
	require.True(t, mgr.versionBumped)
}

func TestMigrationManager_BumpBlockSubsequentCallsPassthrough(t *testing.T) {
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

	// Drive to complete + bump.
	require.NoError(t, mgr.ApplyChangeSets(context.Background(), nil)) // migrate a
	require.NoError(t, mgr.ApplyChangeSets(context.Background(), nil)) // boundary -> Complete
	require.NoError(t, mgr.ApplyChangeSets(context.Background(), nil)) // bump block
	require.True(t, mgr.versionBumped)

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
