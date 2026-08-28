package operations

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

func TestOpenMemiAVLReplayReadOnlyRefusesATornChangelogWithoutRepair(t *testing.T) {
	homeDir := t.TempDir()
	store := newTestMemiavlStore(t, homeDir)
	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{noncePair(addrN(0xA1), 1)}},
	}}))
	_, err := store.Commit(store.Version() + 1)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	dbDir := utils.GetCosmosSCStorePath(homeDir)
	segment := lastOperationsMemiAVLWALSegment(t, dbDir)
	committed, err := os.ReadFile(filepath.Clean(segment))
	require.NoError(t, err)
	require.NotEmpty(t, committed)
	before := committed[:len(committed)-1]
	require.NoError(t, os.WriteFile(filepath.Clean(segment), before, 0o600))

	_, err = openMemiAVLReplayReadOnly(dbDir, 0)
	require.Error(t, err)
	require.ErrorIs(t, err, wal.ErrCorrupt)
	require.Contains(t, err.Error(), "left unmodified")

	after, readErr := os.ReadFile(filepath.Clean(segment))
	require.NoError(t, readErr)
	require.Equal(t, before, after)
}

// TestOpenMemiAVLReplayReadOnlyRefusesADirectoryAWriterHasOpen covers what a
// pre-open check cannot: while a writer holds the directory, the changelog
// opener can complete a truncation that writer is partway through, which leaves
// its log marked corrupt. Replaying a live directory is refused outright.
func TestOpenMemiAVLReplayReadOnlyRefusesADirectoryAWriterHasOpen(t *testing.T) {
	homeDir := t.TempDir()
	store := newTestMemiavlStore(t, homeDir)
	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{noncePair(addrN(0xA1), 1)}},
	}}))
	_, err := store.Commit(store.Version() + 1)
	require.NoError(t, err)

	// The store is still open, holding the lock the way a running seid does.
	dbDir := utils.GetCosmosSCStorePath(homeDir)
	db, err := openMemiAVLReplayReadOnly(dbDir, 0)
	require.Nil(t, db)
	require.ErrorIs(t, err, memiavl.ErrLocked)
	require.Contains(t, err.Error(), "stop seid and rerun")
	require.Contains(t, err.Error(), "--memiavl-open-mode snapshot")

	// The same directory opens once that writer releases it.
	require.NoError(t, store.Close())
	db, err = openMemiAVLReplayReadOnly(dbDir, 0)
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

// TestOpenMemiAVLReplayReadOnlyAcceptsACompleteChangelog is the positive
// control for the pre-flight check: a changelog a real store wrote and closed
// opens and replays as it did before the check existed.
func TestOpenMemiAVLReplayReadOnlyAcceptsACompleteChangelog(t *testing.T) {
	homeDir := t.TempDir()
	store := newTestMemiavlStore(t, homeDir)
	for nonce := uint64(1); nonce <= 3; nonce++ {
		require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
			Name:      keys.EVMStoreKey,
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{noncePair(addrN(0xA1), nonce)}},
		}}))
		_, err := store.Commit(store.Version() + 1)
		require.NoError(t, err)
	}
	require.NoError(t, store.Close())

	db, err := openMemiAVLReplayReadOnly(utils.GetCosmosSCStorePath(homeDir), 3)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.Equal(t, int64(3), db.Version())
}

func lastOperationsMemiAVLWALSegment(t *testing.T, dbDir string) string {
	t.Helper()
	changelogDir := utils.GetChangelogPath(dbDir)
	entries, err := os.ReadDir(changelogDir)
	require.NoError(t, err)
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && len(entry.Name()) == 20 {
			names = append(names, entry.Name())
		}
	}
	require.NotEmpty(t, names)
	sort.Strings(names)
	return filepath.Join(changelogDir, names[len(names)-1])
}
