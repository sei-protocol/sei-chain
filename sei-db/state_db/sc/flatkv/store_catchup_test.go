package flatkv

import (
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/stretchr/testify/require"
)

// TestCatchupAllowsVersionGaps verifies that catchup can jump from
// committedVersion to a later WAL entry when intermediate heights were never
// committed (gapped / batched CommitBlock, or empty commits that jump ahead).
func TestCatchupAllowsVersionGaps(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	for i := byte(1); i <= 5; i++ {
		commitStorageEntry(t, s, ktype.Address{i}, ktype.Slot{i}, []byte{i})
	}
	require.Equal(t, int64(5), s.committedVersion)

	off, err := s.walOffsetForVersion(4)
	require.NoError(t, err)
	require.NoError(t, s.changelog.TruncateBefore(off))

	// Simulate a store whose committed watermark is behind the truncated WAL
	// tip. Catchup should advance by replaying v4/v5 even though v3 is absent.
	s.committedVersion = 2

	require.NoError(t, s.catchup(0))
	require.Equal(t, int64(5), s.committedVersion)
}

// TestCatchupNoOpWhenWALBehindCommittedVersion verifies catchup is a clean
// no-op when the WAL only contains entries that are already covered by
// committedVersion (the normal post-truncation steady state).
func TestCatchupNoOpWhenWALBehindCommittedVersion(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	for i := byte(1); i <= 3; i++ {
		commitStorageEntry(t, s, ktype.Address{i}, ktype.Slot{i}, []byte{i})
	}
	require.Equal(t, int64(3), s.committedVersion)

	require.NoError(t, s.catchup(0))
	require.Equal(t, int64(3), s.committedVersion)
}

// TestCatchupRecoversGappedCommitBlockAfterMetadataLag simulates the crash
// window after Commit Step 1 (WAL write) / Step 2 (per-DB commit) but before
// Step 3 (global metadata): per-DB state and WAL are at a gapped height while
// the in-memory/global watermark still lags. Catchup must apply the gapped
// WAL entry instead of aborting with "WAL hole"/"WAL gap".
func TestCatchupRecoversGappedCommitBlockAfterMetadataLag(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := ktype.Address{0xAB}
	slot := ktype.Slot{0xCD}
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
	cs := makeChangeSet(key, padLeft32(0x11), false)

	_, err = s.CommitBlock(10, []*proto.NamedChangeSet{cs})
	require.NoError(t, err)
	require.Equal(t, int64(10), s.Version())
	hashAfterCommit := append([]byte(nil), s.RootHash()...)

	// Rewind only the global watermark to mimic metadata lagging the WAL /
	// per-DB commits. Catchup should replay the gapped WAL entry at v10.
	s.committedVersion = 0
	require.NoError(t, s.catchup(0))
	require.Equal(t, int64(10), s.committedVersion)
	require.Equal(t, hashAfterCommit, s.RootHash())

	height, found, err := s.GetBlockHeightModified(keys.EVMStoreKey, key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(10), height)

	require.NoError(t, s.Close())
}
