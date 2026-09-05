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

	require.NoError(t, s.replayIntoMutableStore(0))
	require.Equal(t, int64(3), s.committedVersion)
}

// TestCatchupReplaysAlreadyAppliedBlockOnSeededStore pins replay over a store whose history begins
// mid-chain. The WAL's first block is the seeded height, so replay must start there rather than treat
// blocks 1..seed-1 as missing, and re-applying a block the DBs already hold must land on the same root
// it had before.
//
// The lagging watermark is written to disk rather than poked into memory: the store derives its
// version from the per-DB records, so an in-memory-only value is a state no crash can produce.
func TestCatchupReplaysAlreadyAppliedBlockOnSeededStore(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())

	addr := ktype.Address{0xAB}
	slot := ktype.Slot{0xCD}
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
	cs := makeChangeSet(key, padLeft32(0x11), false)

	// History legally begins at 10, so this is a lagging watermark rather than a store that skipped
	// blocks 1-9.
	require.NoError(t, s.SetInitialVersion(10))
	require.NoError(t, s.CommitStateChanges(10, []*proto.NamedChangeSet{cs}))
	require.Equal(t, int64(10), s.Version())
	hashAfterCommit := append([]byte(nil), rootHash(s)...)

	rewindVersionRecords(t, s, 9)
	require.NoError(t, s.Close())

	reopened, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	defer reopened.Close()

	require.NoError(t, reopened.LoadLatest())
	require.Equal(t, int64(10), reopened.Version())
	require.Equal(t, hashAfterCommit, rootHash(reopened))

	height, found, err := reopened.GetBlockHeightModified(keys.EVMStoreKey, key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(10), height)
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
	require.NoError(t, s.CommitStateChanges(firstBlock, []*proto.NamedChangeSet{cs}))
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
	err := s.replayIntoMutableStore(0)
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
	require.NoError(t, s.sealBaseline(), "re-seal the baseline at the rewound height, as open would")
	require.NoError(t, s.replayIntoMutableStore(0))
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
	require.NoError(t, s.CommitStateChanges(10, []*proto.NamedChangeSet{cs}))

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
		require.NoError(t, s.CommitStateChanges(v, []*proto.NamedChangeSet{cs}))
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
		require.NoError(t, s.CommitStateChanges(v, []*proto.NamedChangeSet{makeChangeSet(key, padLeft32(val), false)}))
	}
	for v := int64(1); v <= 4; v++ {
		commit(v, byte(v))
	}

	// CommitStateChanges offers snapshots to the writer without waiting, so wait here: the snapshots this
	// falls back to have to be on disk before the WAL is wiped.
	require.NoError(t, s.FlushSnapshots())

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
// applied. Catchup reports it as the replayed-blocks metric and in its progress log, so an off-by-one here
// misreports how much recovery a restart actually did.
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

	// Rewind the in-memory watermark so the WAL holds blocks the store must re-apply, and re-seal the
	// baseline there, which is the pair of things open does before it replays.
	s.committedVersion = 1
	require.NoError(t, s.sealBaseline())
	it, ok, err := s.openReplayIterator(s.committedVersion, 0)
	require.NoError(t, err)
	require.True(t, ok)

	replayed, err := replayBlocks(s, it, nil)
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
	primaryHash := append([]byte(nil), rootHash(s)...)

	ro, err := s.LoadVersionReadOnly(2)
	require.NoError(t, err)
	defer func() { require.NoError(t, ro.Close()) }()

	require.Equal(t, int64(2), ro.Version(), "the clone must land exactly on the requested version")
	require.Equal(t, primaryVersion, s.committedVersion, "feeding a clone must not move the primary")
	require.Equal(t, primaryHash, rootHash(s))
}

// A store that already holds the block being replayed must not have its recorded height written
// backwards. Catch-up feeds each block only to the stores that need it, but the seal that follows
// records metadata for every store, so a store sitting at a later height gets a note claiming an
// earlier one — paired with the hash of the height it actually holds. The two halves of that note then
// describe different moments, and if the process dies mid-catch-up it is the note that survives.
//
// The skew is the skip list, which is an argument to applyAndCommit, so no partial flush needs
// manufacturing: hand it a list that marks the other stores as already holding a later block.
func TestReplaySkipDoesNotRewindRecordedHeight(t *testing.T) {
	s := setupTestStore(t)
	defer func() { _ = s.Close() }()

	for round := byte(1); round <= 4; round++ {
		commitMixedState(t, s, round)
	}
	requireFlushedToDisk(t, s)
	require.Equal(t, int64(4), s.Version())

	// What each database recorded at block 4, which is the state it must keep.
	before := make(map[string]*ktype.LocalMeta, len(dataDBDirs))
	for _, dir := range dataDBDirs {
		meta, err := loadLocalMeta(s.rawDBFor(dir))
		require.NoError(t, err)
		require.Equal(t, int64(4), meta.CommittedVersion, "%s must start at block 4", dir)
		before[dir] = meta
	}

	// Replay block 3 with only the storage database behind. Every other store already holds block 4
	// and is skipped, exactly as a catch-up after a partial flush would do.
	skipped := []string{accountDBDir, codeDBDir, miscDBDir}
	alreadyHave := map[string]int64{
		accountDBDir: 4, codeDBDir: 4, miscDBDir: 4,
		storageDBDir: 2,
	}
	// Open would have derived the committed version from the lowest height any store reached and sealed
	// the baseline there, which is what a catch-up replays forward from.
	s.committedVersion = 2
	require.NoError(t, s.sealBaseline())

	addr, slot := addrN(3), slotN(3)
	block3 := []*proto.NamedChangeSet{namedCS(
		noncePair(addr, 3),
		codeHashPair(addr, codeHashN(3)),
		codePair(addr, []byte{0x60, 0x80, 3}),
		storagePair(addr, slot, []byte{3, 0xAA}),
	)}
	require.NoError(t, s.applyAndCommit(3, block3, alreadyHave))
	requireFlushedToDisk(t, s)

	for _, dir := range skipped {
		meta, err := loadLocalMeta(s.rawDBFor(dir))
		require.NoError(t, err)
		require.Equal(t, int64(4), meta.CommittedVersion,
			"%s skipped block 3, so its recorded height must not be rewound to 3", dir)
		require.True(t, before[dir].LtHash.Equal(meta.LtHash),
			"%s skipped block 3, so its recorded hash must not change", dir)
	}
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
	require.NoError(t, s.CommitStateChanges(1, []*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{noncePair(addr, 7)}},
	}}))
	require.NoError(t, s.CommitStateChanges(2, []*proto.NamedChangeSet{{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, ktype.Slot{0x01})),
				Value: padLeft32(0x22)},
		}},
	}}))
	require.NoError(t, s.CommitStateChanges(3, []*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{codePair(addr, []byte{0x60, 0x0A})}},
	}}))

	wantRoot := bytes.Clone(rootHash(s))
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
	require.Equal(t, wantRoot, rootHash(s3),
		"rebuilding an account row through partial-field replays must land on the same root")
	gotAccount, found := s3.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]))
	require.True(t, found)
	require.Equal(t, wantAccount, gotAccount)
	require.NoError(t, VerifyLtHash(s3))
}
