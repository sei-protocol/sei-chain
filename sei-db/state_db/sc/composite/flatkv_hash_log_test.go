package composite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// flatKV hashes asynchronously and this store is the only consumer of those hashes on the Cosmos
// path, so the hash log gets them here or nowhere. The columns are compared against the ones this
// store declares, since a hash written under a name nothing declares is a column no reader looks at,
// and a declared column nothing writes holds every block out of the archive.
func TestFlatKVHashesReachTheHashLog(t *testing.T) {
	const blocks = 2

	archiveDir := t.TempDir()
	loggerConfig := hashlog.DefaultHashLoggerConfig(archiveDir, "composite-hash-log-test")
	loggerConfig.HashTypes = flatkv.HashTypes()
	hl, err := hashlog.NewHashLogger(loggerConfig)
	require.NoError(t, err)

	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.FlatKVOnly

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg, hl)
	require.NoError(t, err)
	require.NoError(t, cs.LoadLatest())
	defer func() { _ = cs.Close() }()

	categories := cs.HashCategories()
	require.Equal(t, flatkv.HashTypes(), categories,
		"FlatKVOnly declares flatKV's columns and nothing else")

	for height := int64(1); height <= blocks; height++ {
		require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("key"), Value: []byte{byte(height)}},
			}}},
		}))
		committed, err := cs.Commit(height)
		require.NoError(t, err)
		require.Equal(t, height, committed)

		// The changeset column is the logger's own and only the caller can supply it. Without it no
		// block is complete and none reaches disk. baseapp plays this part in production.
		hl.ReportChangeset(uint64(height), nil)
	}

	require.NoError(t, hl.Close())

	for height := uint64(1); height <= blocks; height++ {
		reports, err := hashlog.ReadHashForBlock(archiveDir, height)
		require.NoError(t, err)
		require.Len(t, reports, 1, "block %d should appear exactly once in the archive", height)

		for _, category := range categories {
			require.NotEmpty(t, reports[0].Hashes[category],
				"block %d recorded no %s hash", height, category)
		}
	}
}
