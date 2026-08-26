package flatkv

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

// GetLatestVersion answers, without opening the store, the version a store opened on that directory
// will report. The tests below assert that equivalence directly rather than against hand-picked
// numbers, because the numbers are the thing most likely to drift: the helper reads the current
// snapshot and the WAL tail, while the open path derives its version from the per-DB watermarks and
// then replays the WAL over them. Any state where those two disagree is a bug in one of them.

// requireProbeMatchesOpen closes s, asks GetLatestVersion, then reopens and loads. Both answers must
// agree, and the agreed value is returned so a caller can additionally pin it.
func requireProbeMatchesOpen(t *testing.T, s *CommitStore, cfg *config.Config) int64 {
	t.Helper()
	require.NoError(t, s.Close())

	probed, err := GetLatestVersion(cfg.DataDir)
	require.NoError(t, err)

	reopened, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, reopened.Close()) }()
	require.NoError(t, reopened.LoadLatest())

	require.Equal(t, reopened.Version(), probed,
		"GetLatestVersion must equal the version the store opens at")
	return probed
}

// newProbeStore returns an opened store over a fresh directory, plus its config.
func newProbeStore(t *testing.T) (*CommitStore, *config.Config) {
	t.Helper()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	return s, cfg
}

func TestGetLatestVersionNeverOpenedDirIsZero(t *testing.T) {
	dir := filepath.Join(t.TempDir(), flatkvRootDir)
	probed, err := GetLatestVersion(dir)
	require.NoError(t, err)
	require.Equal(t, int64(0), probed, "a directory that has never been opened reads as 0")
}

func TestGetLatestVersionFreshStore(t *testing.T) {
	s, cfg := newProbeStore(t)
	require.Equal(t, int64(0), requireProbeMatchesOpen(t, s, cfg))
}

func TestGetLatestVersionAfterCommits(t *testing.T) {
	s, cfg := newProbeStore(t)
	for i := int64(1); i <= 3; i++ {
		require.NoError(t, s.CommitBlock(i, []*proto.NamedChangeSet{bankPair([]byte("k"), []byte{byte(i)})}))
	}
	require.Equal(t, int64(3), requireProbeMatchesOpen(t, s, cfg))
}

// TestGetLatestVersionWatermarksBehindWAL is the case the previous implementation got wrong, and the
// one that matters: a crash between the WAL flush and the per-DB batch commits leaves every watermark
// one block behind while the WAL holds the block. The store replays it on open, so the answer is the
// WAL tail. Reading any watermark instead is one low, which is the difference between a legacy store
// upgrade firing and the node exiting at startup.
func TestGetLatestVersionWatermarksBehindWAL(t *testing.T) {
	s, cfg := newProbeStore(t)
	for i := int64(1); i <= 3; i++ {
		require.NoError(t, s.CommitBlock(i, []*proto.NamedChangeSet{bankPair([]byte("k"), []byte{byte(i)})}))
	}
	rewindVersionRecords(t, s, 2)

	// Pin that the two candidate sources genuinely disagree here, so this test fails for an
	// implementation that reads a watermark rather than for an implementation detail.
	for _, ndb := range selectDataDBs(t, s, nil) {
		meta, err := loadLocalMeta(ndb.db)
		require.NoError(t, err)
		require.Equal(t, int64(2), meta.CommittedVersion, "%s watermark trails the WAL", ndb.dir)
	}

	probed := requireProbeMatchesOpen(t, s, cfg)
	require.Equal(t, int64(3), probed, "the WAL still holds block 3, so the store replays to 3")
}

// TestGetLatestVersionSeededStore covers the state with no WAL entries at all: SetInitialVersion
// stamps the watermarks and writes a snapshot, so the snapshot is the only record of the version.
func TestGetLatestVersionSeededStore(t *testing.T) {
	s, cfg := newProbeStore(t)
	require.NoError(t, s.SetInitialVersion(100))
	require.Equal(t, int64(99), requireProbeMatchesOpen(t, s, cfg))
}

// TestGetLatestVersionTornSeed pairs the probe with the discard path: a seed interrupted partway
// leaves watermarks nothing else corroborates, the store discards them and opens at 0, and the probe
// must agree rather than reporting the abandoned height.
func TestGetLatestVersionTornSeed(t *testing.T) {
	s, cfg := newProbeStore(t)
	stampSeedRecords(t, s, 99, accountDBDir, codeDBDir)
	require.Equal(t, int64(0), requireProbeMatchesOpen(t, s, cfg))
}

// TestGetLatestVersionSnapshotBehindWAL pins that the snapshot alone is not the answer: with a
// snapshot well behind the head, the WAL tail carries the version.
func TestGetLatestVersionSnapshotBehindWAL(t *testing.T) {
	s, cfg := newProbeStore(t)
	for i := int64(1); i <= 2; i++ {
		require.NoError(t, s.CommitBlock(i, []*proto.NamedChangeSet{bankPair([]byte("k"), []byte{byte(i)})}))
	}
	require.NoError(t, s.WriteSnapshot(""))
	for i := int64(3); i <= 5; i++ {
		require.NoError(t, s.CommitBlock(i, []*proto.NamedChangeSet{bankPair([]byte("k"), []byte{byte(i)})}))
	}

	snapVersion, err := currentSnapshotVersion(cfg.DataDir)
	require.NoError(t, err)
	require.Equal(t, int64(2), snapVersion, "fixture precondition: the snapshot trails the head")
	require.Equal(t, int64(5), requireProbeMatchesOpen(t, s, cfg))
}

// TestGetLatestVersionWALWipedAfterSnapshot is the mirror of the case above: with the WAL gone, the
// snapshot carries the version.
func TestGetLatestVersionWALWipedAfterSnapshot(t *testing.T) {
	s, cfg := newProbeStore(t)
	for i := int64(1); i <= 2; i++ {
		require.NoError(t, s.CommitBlock(i, []*proto.NamedChangeSet{bankPair([]byte("k"), []byte{byte(i)})}))
	}
	require.NoError(t, s.WriteSnapshot(""))
	resetWALForTest(t, s)

	require.Equal(t, int64(2), requireProbeMatchesOpen(t, s, cfg))
}

// TestCommitStoreGetLatestVersionUsesMemoryWhileOpen pins that the method short-circuits to the
// in-memory watermark while the store is open, so it never contends for the changelog lock the
// free-standing helper takes.
func TestCommitStoreGetLatestVersionUsesMemoryWhileOpen(t *testing.T) {
	s, _ := newProbeStore(t)
	defer func() { require.NoError(t, s.Close()) }()

	require.NoError(t, s.CommitBlock(1, []*proto.NamedChangeSet{bankPair([]byte("k"), []byte("v"))}))
	got, err := s.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(1), got)
}
