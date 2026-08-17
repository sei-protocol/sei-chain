package flatkv

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/stretchr/testify/require"
)

// TestCatchupNoOpWhenWALBehindCommittedVersion verifies catchup is a clean
// no-op when the WAL only contains entries that are already covered by
// committedVersion (the normal post-truncation steady state).
func TestCatchupNoOpWhenWALBehindCommittedVersion(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)
	defer s.Close()

	for i := byte(1); i <= 3; i++ {
		commitStorageEntry(t, s, ktype.Address{i}, ktype.Slot{i}, []byte{i})
	}
	require.Equal(t, int64(3), s.committedVersion)

	_, err = s.replayIntoMutableStore(0)
	require.NoError(t, err)
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
	err = s.LoadLatest()
	require.NoError(t, err)

	addr := ktype.Address{0xAB}
	slot := ktype.Slot{0xCD}
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
	cs := makeChangeSet(key, padLeft32(0x11), false)

	// Seed so history legally begins at 10, then commit it: the crash window this simulates is a lagging
	// watermark, not a store that skipped blocks 1-9.
	require.NoError(t, s.SetInitialVersion(10))
	require.NoError(t, s.CommitBlock(10, []*proto.NamedChangeSet{cs}))
	require.Equal(t, int64(10), s.Version())
	hashAfterCommit := append([]byte(nil), s.RootHash()...)

	// Rewind only the global watermark to mimic metadata lagging the WAL /
	// per-DB commits. Catchup should replay the gapped WAL entry at v10.
	s.committedVersion = 9
	_, err = s.replayIntoMutableStore(0)
	require.NoError(t, err)
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
	err = s.LoadLatest()
	require.NoError(t, err)

	// Seed so history legally begins at firstBlock; committing it directly on a fresh store is rejected,
	// because the first block is 1.
	require.NoError(t, s.SetInitialVersion(firstBlock))

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
	_, err := s.replayIntoMutableStore(0)
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
	_, err := s.replayIntoMutableStore(0)
	require.NoError(t, err)
	require.Equal(t, int64(10), s.committedVersion)
}

// TestLoadVersionSurfacesCatchupGap verifies the gap error reaches the edge of the flatKV API rather than
// being swallowed somewhere between catchup and LoadVersion.
func TestLoadVersionSurfacesCatchupGap(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	require.NoError(t, s.SetInitialVersion(10))
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(ktype.Address{0xAB}, ktype.Slot{0xCD}))
	cs := makeChangeSet(key, padLeft32(0x11), false)
	require.NoError(t, s.CommitBlock(10, []*proto.NamedChangeSet{cs}))

	// Rewind the persisted watermark so the reopened store needs blocks 6-9, which this WAL never held.
	rewindVersionRecords(t, s, 5)
	require.NoError(t, s.Close())

	reopened, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	defer func() { _ = reopened.Close() }()

	err = reopened.LoadLatest()
	require.Error(t, err)
	require.Contains(t, err.Error(), "blocks 6-9 are missing")
}

// TestReadOnlyServesSeededMidChainStore pins that a store whose history legally begins above block 1 — seeded
// by SetInitialVersion, which also writes the snapshot at seededVersion — is still readable at a past height.
// The seeded snapshot is what makes this legal: without it the clone would have no baseline to replay onto.
func TestReadOnlyServesSeededMidChainStore(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close()) }()

	require.NoError(t, s.SetInitialVersion(10))
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(ktype.Address{0xAB}, ktype.Slot{0xCD}))
	for _, v := range []int64{10, 11, 12} {
		cs := makeChangeSet(key, padLeft32(byte(v)), false)
		require.NoError(t, s.CommitBlock(v, []*proto.NamedChangeSet{cs}))
	}

	// The clone opens at the seeded snapshot (version 9) and replays 10-12 from the WAL.
	ro, err := s.LoadVersionReadOnly(12)
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
	err = s.LoadLatest()
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close()) }()

	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(ktype.Address{0xAB}, ktype.Slot{0xCD}))
	commit := func(v int64, val byte) {
		require.NoError(t, s.CommitBlock(v, []*proto.NamedChangeSet{makeChangeSet(key, padLeft32(val), false)}))
	}
	for v := int64(1); v <= 4; v++ {
		commit(v, byte(v))
	}

	// Wipe the WAL and resume, so it no longer reaches back to the snapshot at version 2.
	resetWALForTest(t, s)
	commit(5, 0x99)

	_, err = s.LoadVersionReadOnly(3)
	require.Error(t, err)
	require.Contains(t, err.Error(), "are missing")

	commit(6, 0xAA)
	require.Equal(t, int64(6), s.Version())
}

// TestResolveReplayRangeBounds exercises resolveReplayRange directly. It is the one piece of arithmetic both
// replay destinations share, so a mistake here is a mistake in crash recovery and in read-only export at once.
// The gap case matters most: silently shortening the range instead of erroring would commit a state whose
// LtHash matches no real chain history, which is a consensus fault rather than a crash.
func TestResolveReplayRangeBounds(t *testing.T) {
	// WAL holds exactly block 10.
	s := gappedWALStore(t, 10)
	defer func() { require.NoError(t, s.Close()) }()

	t.Run("replays the single stored block", func(t *testing.T) {
		start, end, ok, err := resolveReplayRange(s.wal, 9, 0)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, uint64(10), start, "replay starts at fromVersion+1")
		require.Equal(t, uint64(10), end)
	})

	t.Run("no-op when the destination is already at the WAL tip", func(t *testing.T) {
		_, _, ok, err := resolveReplayRange(s.wal, 10, 0)
		require.NoError(t, err)
		require.False(t, ok, "nothing past fromVersion is a clean no-op, not an error")
	})

	t.Run("no-op when the destination is past the WAL tip", func(t *testing.T) {
		_, _, ok, err := resolveReplayRange(s.wal, 11, 0)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("no-op when targetVersion is at or below the destination", func(t *testing.T) {
		_, _, ok, err := resolveReplayRange(s.wal, 10, 10)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("targetVersion caps the range", func(t *testing.T) {
		start, end, ok, err := resolveReplayRange(s.wal, 9, 10)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, uint64(10), start)
		require.Equal(t, uint64(10), end, "end is clamped to targetVersion")
	})

	t.Run("a gap is an error, not a shortened range", func(t *testing.T) {
		_, _, ok, err := resolveReplayRange(s.wal, 5, 0)
		require.Error(t, err)
		require.False(t, ok)
		require.Contains(t, err.Error(), "blocks 6-9 are missing")
	})
}

// TestResolveReplayRangeOnEmptyWAL pins that an empty WAL is a clean no-op rather than a gap error: there are no
// blocks to be missing.
func TestResolveReplayRangeOnEmptyWAL(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	defer func() { require.NoError(t, s.Close()) }()

	_, _, ok, err := resolveReplayRange(s.wal, 0, 0)
	require.NoError(t, err)
	require.False(t, ok)
}

// TestReplayBlocksReturnsAppliedCount pins the one value replayBlocks promises on success: how many blocks it
// applied. replayIntoMutableStore uses it both to decide whether to persist the watermark and to report the
// replayed-blocks metric, so an off-by-one here would either skip the watermark commit or misreport progress.
func TestReplayBlocksReturnsAppliedCount(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	defer func() { require.NoError(t, s.Close()) }()

	for i := byte(1); i <= 3; i++ {
		commitStorageEntry(t, s, ktype.Address{i}, ktype.Slot{i}, []byte{i})
	}
	require.Equal(t, int64(3), s.committedVersion)

	// Rewind only the in-memory watermark so the WAL holds blocks the store must re-apply.
	s.committedVersion = 1
	it, ok, err := s.openReplayIterator(s.committedVersion, 0)
	require.NoError(t, err)
	require.True(t, ok)

	replayed, err := replayBlocks(s, it)
	require.NoError(t, err)
	require.Equal(t, 2, replayed, "blocks 2 and 3 must be replayed and counted")
	require.Equal(t, int64(3), s.committedVersion)

	// Nothing left to replay: the resolver reports that, rather than handing back an empty iterator.
	_, ok, err = s.openReplayIterator(s.committedVersion, 0)
	require.NoError(t, err)
	require.False(t, ok)
}

// TestReplayIntoReadOnlyCopyDoesNotDisturbPrimary drives the clone destination through the shared helper and
// checks the asymmetry that justifies the two wrappers: the clone advances, the primary's committed version and
// root hash do not move, and no watermark is written on the clone's behalf.
func TestReplayIntoReadOnlyCopyDoesNotDisturbPrimary(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	defer func() { require.NoError(t, s.Close()) }()

	for i := byte(1); i <= 3; i++ {
		commitStorageEntry(t, s, ktype.Address{i}, ktype.Slot{i}, []byte{i})
	}
	primaryVersion := s.committedVersion
	primaryHash := append([]byte(nil), s.RootHash()...)

	ro, err := s.LoadVersionReadOnly(2)
	require.NoError(t, err)
	defer func() { require.NoError(t, ro.Close()) }()

	require.Equal(t, int64(2), ro.Version(), "the clone must land exactly on the requested version")
	require.Equal(t, primaryVersion, s.committedVersion, "feeding a clone must not move the primary")
	require.Equal(t, primaryHash, s.RootHash())
}

// TestReplayConvergesOnPartialAccountFieldWrites pins the one case where replaying
// a block into a DB that already holds it is not obviously a no-op. An account row
// is a merge, not an overwrite: deriveNewAccountValues folds a nonce-only or
// codehash-only update onto whatever is currently on disk. Replaying a range where
// different blocks touch different fields therefore rebuilds the row field by field
// through intermediate values that were never on-chain. It converges because the
// last block to write each field writes it last — and the LtHash must land on the
// same value either way, since that value feeds the AppHash.
func TestReplayConvergesOnPartialAccountFieldWrites(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dbDir
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())

	addr := ktype.Address{0xAB}
	// Block 1 sets the nonce, block 2 is unrelated, block 3 sets only the codehash.
	require.NoError(t, s.CommitBlock(1, []*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{noncePair(addr, 7)}},
	}}))
	require.NoError(t, s.CommitBlock(2, []*proto.NamedChangeSet{{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, ktype.Slot{0x01})),
				Value: padLeft32(0x22)},
		}},
	}}))
	require.NoError(t, s.CommitBlock(3, []*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{codePair(addr, []byte{0x60, 0x0A})}},
	}}))

	wantRoot := bytes.Clone(s.CommittedRootHash())
	wantAccount, found := s.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]))
	require.True(t, found)
	require.NoError(t, s.Close())

	// Rewind accountDB alone to block 1, leaving its rows at block 3. Replay of
	// blocks 2 and 3 now runs against an account row that already holds both fields.
	s2, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s2.LoadLatest())
	rewindVersionRecords(t, s2, 1, accountDBDir)
	require.NoError(t, s2.Close())

	s3, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s3.LoadLatest())
	defer s3.Close()

	require.Equal(t, int64(3), s3.Version())
	require.Equal(t, wantRoot, s3.CommittedRootHash(),
		"rebuilding an account row through partial-field replays must land on the same root")
	gotAccount, found := s3.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]))
	require.True(t, found)
	require.Equal(t, wantAccount, gotAccount)
	require.NoError(t, VerifyLtHash(s3))
}
