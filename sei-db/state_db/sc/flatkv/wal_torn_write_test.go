package flatkv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/stretchr/testify/require"
)

func TestStoreOpenAfterWALTornWrite(t *testing.T) {
	t.Run("mid_record_truncation_discards_torn_commit", func(t *testing.T) {
		cfg, key, rootAtV4, _ := prepareStoreWithManualWALTail(t)
		truncateLastWALSegmentBy(t, filepath.Join(cfg.DataDir, changelogDir), 4)

		s := reopenTestStore(t, cfg)
		defer func() { require.NoError(t, s.Close()) }()

		require.Equal(t, int64(4), s.Version())
		require.Equal(t, rootAtV4, s.CommittedRootHash())
		val, found := s.Get(keys.EVMStoreKey, key)
		require.True(t, found)
		require.Equal(t, padLeft32(0x04), val)
		verifyLtHashConsistency(t, s)
	})

	t.Run("partial_length_prefix_discards_torn_commit", func(t *testing.T) {
		cfg, key, rootAtV4, tailStart := prepareStoreWithManualWALTail(t)
		replaceManualWALTailWithPartialLengthPrefix(t, filepath.Join(cfg.DataDir, changelogDir), tailStart)

		s := reopenTestStore(t, cfg)
		defer func() { require.NoError(t, s.Close()) }()

		require.Equal(t, int64(4), s.Version())
		require.Equal(t, rootAtV4, s.CommittedRootHash())
		val, found := s.Get(keys.EVMStoreKey, key)
		require.True(t, found)
		require.Equal(t, padLeft32(0x04), val)
		verifyLtHashConsistency(t, s)
	})

	t.Run("clean_tail_replays_last_wal_commit", func(t *testing.T) {
		cfg, key, rootAtV4, _ := prepareStoreWithManualWALTail(t)

		s := reopenTestStore(t, cfg)
		defer func() { require.NoError(t, s.Close()) }()

		require.Equal(t, int64(5), s.Version())
		require.NotEqual(t, rootAtV4, s.CommittedRootHash())
		val, found := s.Get(keys.EVMStoreKey, key)
		require.True(t, found)
		require.Equal(t, padLeft32(0x05), val)
		verifyLtHashConsistency(t, s)
	})
}

func prepareStoreWithManualWALTail(t *testing.T) (*config.Config, []byte, []byte, int64) {
	t.Helper()

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)
	cfg.SnapshotInterval = 0

	s := reopenTestStore(t, cfg)
	addr := ktype.Address{0x44}
	slot := ktype.Slot{0x55}
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
	for v := byte(1); v <= 4; v++ {
		commitStorageEntry(t, s, addr, slot, []byte{v})
	}
	require.Equal(t, int64(4), s.Version())
	rootAtV4 := append([]byte(nil), s.CommittedRootHash()...)
	tailStart := walSegmentSize(t, filepath.Join(cfg.DataDir, changelogDir))

	cs := makeChangeSet(key, padLeft32(0x05), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	require.NoError(t, s.changelog.Write(proto.ChangelogEntry{
		Version:    5,
		Changesets: s.pendingChangeSets,
	}))

	// Simulate a process dying after WAL append and before DB batch commit.
	// Close releases file handles for deterministic test-time file mutation.
	s.clearPendingWrites()
	require.NoError(t, s.Close())
	return cfg, key, rootAtV4, tailStart
}

func reopenTestStore(t *testing.T, cfg *config.Config) *CommitStore {
	t.Helper()
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	return s
}

func truncateLastWALSegmentBy(t *testing.T, walDir string, n int64) {
	t.Helper()
	path := lastWALSegment(t, walDir)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Greater(t, info.Size(), n)
	require.NoError(t, os.Truncate(path, info.Size()-n))
}

func replaceManualWALTailWithPartialLengthPrefix(t *testing.T, walDir string, tailStart int64) {
	t.Helper()
	path := lastWALSegment(t, walDir)
	require.NoError(t, os.Truncate(path, tailStart))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.Write([]byte{0x80})
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func walSegmentSize(t *testing.T, walDir string) int64 {
	t.Helper()
	info, err := os.Stat(lastWALSegment(t, walDir))
	require.NoError(t, err)
	return info.Size()
}

func lastWALSegment(t *testing.T, walDir string) string {
	t.Helper()
	entries, err := os.ReadDir(walDir)
	require.NoError(t, err)
	var last string
	for _, entry := range entries {
		if entry.IsDir() || len(entry.Name()) < 20 {
			continue
		}
		last = entry.Name()
	}
	require.NotEmpty(t, last, "expected at least one WAL segment in %s", walDir)
	return filepath.Join(walDir, last)
}
