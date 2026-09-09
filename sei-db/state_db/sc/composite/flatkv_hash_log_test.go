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
//
// The hashes go in through RecordHashes rather than a listener, which is what keeps a row complete:
// a listener also hears the heights flatKV reaches while opening and replaying, which no block of
// this archive has the rest of the columns for.
func TestFlatKVHashesReachTheHashLog(t *testing.T) {
	const blocks = 2

	archiveDir := t.TempDir()
	loggerConfig := hashlog.DefaultHashLoggerConfig(archiveDir, "composite-hash-log-test")
	loggerConfig.HashTypes = flatkv.HashTypes()
	hl, err := hashlog.NewHashLogger(loggerConfig)
	require.NoError(t, err)

	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.FlatKVOnly

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
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

		// Both of these are rootmulti's part in production, right after Commit: the backends' hashes,
		// and the changeset column, which is the logger's own and only a caller can supply.
		require.NoError(t, cs.RecordHashes(hl, uint64(height)))
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

// A store reaching a height is not a block being committed. flatKV announces the height it starts
// hashing from and every block it replays, and a row for one of those would carry flatKV's columns
// and nothing else — the app hash, the changeset and the rest only exist for a block the node
// commits. Such a row reads back as a divergence, which is why nothing may be written for it.
func TestReopeningWritesNoRowForTheLoadedHeight(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.FlatKVOnly

	first, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, first.LoadLatest())
	commitOneRecordedBlock(t, first, 1, nil)
	require.NoError(t, first.Close())

	// A second logger over the same archive, as a restart produces.
	archiveDir := t.TempDir()
	loggerConfig := hashlog.DefaultHashLoggerConfig(archiveDir, "composite-reopen-test")
	loggerConfig.HashTypes = flatkv.HashTypes()
	// A row missing columns is written only when the buffer overflows, so the bound is dropped to
	// where one extra pending block reaches it. At the default a restart hides the row until a
	// thousand blocks later, and a clean shutdown discards it instead.
	loggerConfig.MaxBufferedBlocks = 1
	hl, err := hashlog.NewHashLogger(loggerConfig)
	require.NoError(t, err)

	reopened, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, reopened.LoadLatest())
	defer func() { _ = reopened.Close() }()
	require.Equal(t, int64(1), reopened.Version(), "the reopened store stands on the block committed above")

	commitOneRecordedBlock(t, reopened, 2, hl)
	require.NoError(t, hl.Close())

	loaded, err := hashlog.ReadHashForBlock(archiveDir, 1)
	require.NoError(t, err)
	require.Empty(t, loaded, "the height the store reopened at is not a block this run committed")

	committed, err := hashlog.ReadHashForBlock(archiveDir, 2)
	require.NoError(t, err)
	require.Len(t, committed, 1, "the block this run committed is recorded")
}

// commitOneRecordedBlock commits one block writing a single EVM key, recording it on hl when one is
// given, the way rootmulti does right after Commit.
func commitOneRecordedBlock(t *testing.T, cs *CompositeCommitStore, height int64, hl hashlog.HashLogger) {
	t.Helper()
	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("key"), Value: []byte{byte(height)}},
		}}},
	}))
	committed, err := cs.Commit(height)
	require.NoError(t, err)
	require.Equal(t, height, committed)

	if hl == nil {
		return
	}
	require.NoError(t, cs.RecordHashes(hl, uint64(height)))
	hl.ReportChangeset(uint64(height), nil)
}
