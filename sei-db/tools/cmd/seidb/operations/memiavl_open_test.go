package operations

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

// TestOpenMemiAVLReplayReadOnlyRefusesATornChangelogWithoutTruncatingIt pins the
// reason replay does not repair: a record that ends mid-write looks the same
// whether the node crashed or is committing right now, and truncating it in the
// second case discards a committed block. The refusal is only worth anything if
// the tail survives it, so the segment's bytes are compared too.
func TestOpenMemiAVLReplayReadOnlyRefusesATornChangelogWithoutTruncatingIt(t *testing.T) {
	homeDir := t.TempDir()
	writeMemiavlNonces(t, homeDir, 3)

	dbDir := utils.GetCosmosSCStorePath(homeDir)
	segment := lastChangelogSegment(t, dbDir)
	torn := tearFileTail(t, segment)

	db, err := openMemiAVLReplayReadOnly(dbDir, 3)
	require.Nil(t, db)
	require.ErrorIs(t, err, wal.ErrCorrupt)
	require.Contains(t, err.Error(), "rerun this command")

	after, err := os.ReadFile(segment) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	require.Equal(t, torn, after, "the open must leave the changelog segment as it found it")
}

// TestOpenMemiAVLReplayReadOnlyReplaysAnIntactChangelog is the positive control:
// refusing a torn tail is only the intended change if an intact one still
// replays.
func TestOpenMemiAVLReplayReadOnlyReplaysAnIntactChangelog(t *testing.T) {
	homeDir := t.TempDir()
	writeMemiavlNonces(t, homeDir, 3)

	db, err := openMemiAVLReplayReadOnly(utils.GetCosmosSCStorePath(homeDir), 3)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.Equal(t, int64(3), db.Version())
}

// writeMemiavlNonces commits count blocks to a fresh memiavl store under homeDir
// and closes it, leaving a changelog with one entry per block.
func writeMemiavlNonces(t *testing.T, homeDir string, count uint64) {
	t.Helper()
	store := newTestMemiavlStore(t, homeDir)
	for nonce := uint64(1); nonce <= count; nonce++ {
		require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
			Name:      keys.EVMStoreKey,
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{noncePair(addrN(0xA1), nonce)}},
		}}))
		_, err := store.Commit(store.Version() + 1)
		require.NoError(t, err)
	}
	require.NoError(t, store.Close())
}

// lastChangelogSegment returns the path of the segment the changelog appends to.
// Segment names are zero-padded indices, so the highest name sorts last.
func lastChangelogSegment(t *testing.T, dbDir string) string {
	t.Helper()
	changelogDir := utils.GetChangelogPath(dbDir)
	entries, err := os.ReadDir(changelogDir)
	require.NoError(t, err)
	var name string
	for _, entry := range entries {
		if !entry.IsDir() && len(entry.Name()) >= 20 {
			name = entry.Name()
		}
	}
	require.NotEmpty(t, name, "no changelog segment under %s", changelogDir)
	return filepath.Join(changelogDir, name)
}

// tearFileTail drops the last byte of path, which is what a reader sees partway
// through the writer's append, and returns the resulting contents.
func tearFileTail(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	require.NotEmpty(t, data)
	torn := data[:len(data)-1]
	require.NoError(t, os.WriteFile(path, torn, 0o600))
	return torn
}
