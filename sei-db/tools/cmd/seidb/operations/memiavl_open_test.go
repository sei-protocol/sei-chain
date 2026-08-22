package operations

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

func TestOpenMemiAVLReplayReadOnlyRefusesATornChangelogWithoutRepair(t *testing.T) {
	homeDir := t.TempDir()
	store := newTestMemiavlStore(t, homeDir)
	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{noncePair(addrN(0xA1), 1)}},
	}}))
	_, err := store.Commit()
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
	require.Contains(t, err.Error(), "live WAL was not modified")
	require.Contains(t, err.Error(), "rerun the command")

	after, readErr := os.ReadFile(filepath.Clean(segment))
	require.NoError(t, readErr)
	require.Equal(t, before, after)
}

func TestOpenMemiAVLReplayReadOnlyAcceptsAFullyCoveredHeight(t *testing.T) {
	homeDir := t.TempDir()
	store := newTestMemiavlStore(t, homeDir)
	for nonce := uint64(1); nonce <= 3; nonce++ {
		require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
			Name:      keys.EVMStoreKey,
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{noncePair(addrN(0xA1), nonce)}},
		}}))
		_, err := store.Commit()
		require.NoError(t, err)
	}
	require.NoError(t, store.Close())

	db, err := openMemiAVLReplayReadOnly(utils.GetCosmosSCStorePath(homeDir), 3)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.Equal(t, int64(3), db.Version())
}

// TestOpenMemiAVLReplayReadOnlyRejectsAPrunedChangelogGap covers a changelog
// pruned past the snapshot. Replay reaches the requested height from a
// contiguous suffix, so the final version looks correct while the versions
// between the snapshot and the changelog's first entry were never applied.
func TestOpenMemiAVLReplayReadOnlyRejectsAPrunedChangelogGap(t *testing.T) {
	homeDir := t.TempDir()
	store := newTestMemiavlStore(t, homeDir)
	commit := func(nonce uint64) {
		require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
			Name:      keys.EVMStoreKey,
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{noncePair(addrN(0xA1), nonce)}},
		}}))
		_, err := store.Commit()
		require.NoError(t, err)
	}
	commit(1)
	commit(2)
	// Snapshot version 2 so the evm tree survives without the changelog entry
	// that creates it, leaving the pruned gap as the only defect.
	require.NoError(t, store.GetDB().RewriteSnapshot(context.Background()))
	commit(3)
	commit(4)
	commit(5)
	require.NoError(t, store.Close())

	dbDir := utils.GetCosmosSCStorePath(homeDir)
	changelog, err := wal.NewChangelogWAL(utils.GetChangelogPath(dbDir), wal.Config{})
	require.NoError(t, err)
	require.NoError(t, changelog.TruncateBefore(5))
	require.NoError(t, changelog.Close())

	db, err := openMemiAVLReplayReadOnly(dbDir, 5)
	if db != nil {
		_ = db.Close()
	}
	require.Error(t, err)
	require.Contains(t, err.Error(), "would be missing from the replay")
	require.Contains(t, err.Error(), "--memiavl-open-mode snapshot")
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
