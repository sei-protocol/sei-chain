package flatkv

import (
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/stretchr/testify/require"
)

// TestCatchupNoOpWhenWALBehindCommittedVersion verifies catchup is a clean
// no-op when the WAL only contains entries that are already covered by
// committedVersion (the normal post-truncation steady state).
func TestCatchupNoOpWhenWALBehindCommittedVersion(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
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

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := ktype.Address{0xAB}
	slot := ktype.Slot{0xCD}
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
	cs := makeChangeSet(key, padLeft32(0x11), false)

	require.NoError(t, s.CommitBlock(10, []*proto.NamedChangeSet{cs}))
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

// gappedWALStore returns a store whose WAL holds exactly one block, at firstBlock, with nothing before it.
// Committing block 10 as the very first commit is legitimate on its own (a chain whose initial_height is
// above 1, or a store materialized mid-chain); callers create the gap by moving committedVersion.
func gappedWALStore(t *testing.T, firstBlock int64) *CommitStore {
	t.Helper()
	cfg := config.DefaultTestConfig(t)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(ktype.Address{0xAB}, ktype.Slot{0xCD}))
	cs := makeChangeSet(key, padLeft32(0x11), false)
	require.NoError(t, s.CommitBlock(firstBlock, []*proto.NamedChangeSet{cs}))
	return s
}

// TestCatchupRejectsWALStartingAfterReplayStart verifies catchup refuses to replay when the WAL no longer
// reaches back to the block after committedVersion. Replaying from the WAL's first block would silently skip
// the missing blocks and commit a state whose LtHash matches no real chain history.
func TestCatchupRejectsWALStartingAfterReplayStart(t *testing.T) {
	s := gappedWALStore(t, 10)
	defer func() { require.NoError(t, s.Close()) }()

	// Replay must start at 6, but the WAL begins at 10: blocks 6-9 are gone.
	s.committedVersion = 5
	err := s.catchup(0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "blocks 6-9 are missing")
	require.Equal(t, int64(5), s.committedVersion, "committedVersion must not advance over a gap")
}

// TestCatchupAcceptsWALStartingExactlyAtReplayStart pins the boundary: a WAL whose first block is exactly
// committedVersion+1 has no gap and must replay. An off-by-one here would reject healthy stores.
func TestCatchupAcceptsWALStartingExactlyAtReplayStart(t *testing.T) {
	s := gappedWALStore(t, 10)
	defer func() { require.NoError(t, s.Close()) }()

	s.committedVersion = 9
	require.NoError(t, s.catchup(0))
	require.Equal(t, int64(10), s.committedVersion)
}

// TestLoadVersionSurfacesCatchupGap verifies the gap error reaches the edge of the flatKV API rather than
// being swallowed somewhere between catchup and LoadVersion.
func TestLoadVersionSurfacesCatchupGap(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(ktype.Address{0xAB}, ktype.Slot{0xCD}))
	cs := makeChangeSet(key, padLeft32(0x11), false)
	require.NoError(t, s.CommitBlock(10, []*proto.NamedChangeSet{cs}))

	// Rewind the persisted watermark so the reopened store needs blocks 6-9, which this WAL never held.
	require.NoError(t, s.commitGlobalMetadata(5, lthash.New()))
	require.NoError(t, s.Close())

	reopened, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	defer func() { _ = reopened.Close() }()

	_, err = reopened.LoadVersion(0, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "blocks 6-9 are missing")
}

// TestReadOnlyAcceptsMidChainWALStart pins the clamp replayInto needs: a clone that opens with no history
// behind it must treat the WAL's first block as the start of history, not as a gap. Without the clamp every
// historical read on a store whose first block is above 1 would be rejected.
func TestReadOnlyAcceptsMidChainWALStart(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close()) }()

	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(ktype.Address{0xAB}, ktype.Slot{0xCD}))
	for _, v := range []int64{10, 11, 12} {
		cs := makeChangeSet(key, padLeft32(byte(v)), false)
		require.NoError(t, s.CommitBlock(v, []*proto.NamedChangeSet{cs}))
	}

	// The only snapshot is the initial one, so the clone opens at version 0 while the WAL begins at 10.
	ro, err := s.LoadVersion(12, true)
	require.NoError(t, err)
	defer func() { require.NoError(t, ro.Close()) }()
	require.Equal(t, int64(12), ro.Version())
}

// TestReadOnlySurfacesReplayGap verifies a clone that holds a real position is refused when the primary's WAL
// no longer reaches back to it, instead of being served with a hole in it. Only the export fails: the primary
// keeps committing, since its own state is untouched.
func TestReadOnlySurfacesReplayGap(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	// Snapshot every other block and retain them, so the clone below can open at version 2.
	cfg.SnapshotInterval = 2
	cfg.SnapshotKeepRecent = 8
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close()) }()

	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(ktype.Address{0xAB}, ktype.Slot{0xCD}))
	commit := func(v int64, val byte) {
		require.NoError(t, s.CommitBlock(v, []*proto.NamedChangeSet{makeChangeSet(key, padLeft32(val), false)}))
	}
	for v := int64(1); v <= 4; v++ {
		commit(v, byte(v))
	}

	// Wipe the WAL and resume far ahead so it no longer reaches back to the snapshot at version 2.
	require.NoError(t, s.resetWAL())
	commit(10, 0x99)

	_, err = s.LoadVersion(3, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "are missing")

	commit(11, 0xAA)
	require.Equal(t, int64(11), s.Version())
}
