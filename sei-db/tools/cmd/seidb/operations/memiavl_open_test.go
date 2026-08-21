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
)

func TestOpenMemiAVLReplayReadOnlyReportsRetryWithoutRepair(t *testing.T) {
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
	file, err := os.OpenFile(filepath.Clean(segment), os.O_WRONLY|os.O_APPEND, 0)
	require.NoError(t, err)
	_, err = file.Write([]byte{0x10})
	require.NoError(t, err)
	require.NoError(t, file.Close())
	before, err := os.ReadFile(filepath.Clean(segment))
	require.NoError(t, err)

	_, err = openMemiAVLReplayReadOnly(dbDir, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "live WAL was not modified")
	require.Contains(t, err.Error(), "rerun the command")

	after, readErr := os.ReadFile(filepath.Clean(segment))
	require.NoError(t, readErr)
	require.Equal(t, before, after)
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
