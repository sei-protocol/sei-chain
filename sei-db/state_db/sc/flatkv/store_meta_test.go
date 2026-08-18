package flatkv

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// =============================================================================
// LocalMeta and Global Metadata
// =============================================================================

func TestLoadLocalMeta(t *testing.T) {
	t.Run("NewDB_ReturnsDefault", func(t *testing.T) {
		db := setupTestDB(t)
		defer db.Close()

		meta, err := loadLocalMeta(db)
		require.NoError(t, err)
		require.NotNil(t, meta)
		require.Equal(t, int64(0), meta.CommittedVersion)
	})

	t.Run("ExistingMeta_LoadsCorrectly", func(t *testing.T) {
		db := setupTestDB(t)
		defer db.Close()

		require.NoError(t, db.Set(ktype.MetaVersionKey, versionToBytes(42), types.WriteOptions{}))

		// Load it back
		loaded, err := loadLocalMeta(db)
		require.NoError(t, err)
		require.Equal(t, int64(42), loaded.CommittedVersion)
		require.Nil(t, loaded.LtHash)
	})

	t.Run("CorruptedVersion_ReturnsError", func(t *testing.T) {
		db := setupTestDB(t)
		defer db.Close()

		require.NoError(t, db.Set(ktype.MetaVersionKey, []byte{0x01, 0x02}, types.WriteOptions{}))

		_, err := loadLocalMeta(db)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid _meta/version length")
	})
}

// TestValidatePerModuleMetadata pins the load-time guard's decision matrix:
// empty module map with a non-identity root (pre-per-module store) and a
// non-empty module map that does not sum to the root (incomplete / drifted
// bookkeeping) are rejected; both would otherwise silently corrupt the
// per-DB root — and thus the global store hash / AppHash — on the first write.
func TestValidatePerModuleMetadata(t *testing.T) {
	nonZero, _ := lthash.ComputeLtHash(nil, []lthash.KVPairWithLastValue{
		{Key: []byte("k"), Value: []byte("v")},
	})
	require.False(t, nonZero.IsZero(), "precondition: crafted root must be non-identity")

	other, _ := lthash.ComputeLtHash(nil, []lthash.KVPairWithLastValue{
		{Key: []byte("other"), Value: []byte("w")},
	})
	require.False(t, other.IsZero())
	require.False(t, nonZero.Equal(other), "precondition: distinct hashes")

	// Two-module root: sum equals root only when both modules are present.
	combined := nonZero.Clone()
	combined.MixIn(other)

	cases := []struct {
		name       string
		meta       *ktype.LocalMeta
		wantErrSub string // empty => expect success
	}{
		{"nil meta", nil, ""},
		{"nil root", &ktype.LocalMeta{}, ""},
		{"identity root, no modules", &ktype.LocalMeta{LtHash: lthash.New()}, ""},
		{
			"non-identity root with matching modules",
			&ktype.LocalMeta{LtHash: nonZero.Clone(), ModuleLtHashes: map[string]*lthash.LtHash{"EVM": nonZero.Clone()}},
			"",
		},
		{
			"multi-module root with matching modules",
			&ktype.LocalMeta{
				LtHash: combined.Clone(),
				ModuleLtHashes: map[string]*lthash.LtHash{
					"EVM":  nonZero.Clone(),
					"bank": other.Clone(),
				},
			},
			"",
		},
		{"non-identity root without modules", &ktype.LocalMeta{LtHash: nonZero.Clone()}, "predates per-module hashing"},
		{
			"modules do not sum to root",
			&ktype.LocalMeta{LtHash: combined.Clone(), ModuleLtHashes: map[string]*lthash.LtHash{"EVM": nonZero.Clone()}},
			"do not sum to per-DB root",
		},
		{
			"identity root with non-zero modules",
			&ktype.LocalMeta{LtHash: lthash.New(), ModuleLtHashes: map[string]*lthash.LtHash{"EVM": nonZero.Clone()}},
			"do not sum to per-DB root",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePerModuleMetadata(storageDBDir, tc.meta)
			if tc.wantErrSub != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErrSub)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestLoadRejectsStoreMissingPerModuleMetadata drives the guard through the real
// load path: it commits data, strips the per-module hash keys off disk to
// imitate a pre-per-module store, and confirms reopening fails loudly instead of
// discarding the pre-existing root on the next write.
func TestLoadRejectsStoreMissingPerModuleMetadata(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultConfig()
	cfg.DataDir = dbDir
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	// A committed EVM storage entry gives storageDB a non-identity per-DB root
	// plus an "EVM" per-module hash.
	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})

	// Simulate a store written before per-module hashing: strip every
	// per-module meta key (hashes + stats) while keeping the per-DB root.
	requireFlushedToDisk(t, s)
	iter, err := s.rawDBFor(storageDBDir).NewIter(&types.IterOptions{
		LowerBound: ktype.ModuleLtHashPrefixBytes,
		UpperBound: ktype.PrefixEnd(ktype.ModuleLtHashPrefixBytes),
	})
	require.NoError(t, err)
	var keys [][]byte
	for ; iter.Valid(); iter.Next() {
		keys = append(keys, append([]byte(nil), iter.Key()...))
	}
	require.NoError(t, iter.Error())
	require.NoError(t, iter.Close())
	require.NotEmpty(t, keys, "precondition: storageDB must carry per-module meta keys")
	for _, k := range keys {
		require.NoError(t, s.rawDBFor(storageDBDir).Delete(k, types.WriteOptions{}))
	}
	require.NoError(t, s.Close())

	// Reopening must reject the tampered store loudly rather than corrupt it.
	cfg2 := config.DefaultConfig()
	cfg2.DataDir = dbDir
	s2, err := newCommitStoreWithWAL(context.Background(), cfg2)
	require.NoError(t, err)
	defer s2.Close()
	err = s2.LoadLatest()
	require.Error(t, err)
	require.Contains(t, err.Error(), "predates per-module hashing")
}

func TestStoreSealBlockUpdatesLocalMeta(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0x12}
	slot := ktype.Slot{0x34}
	key := evmStorageKey(addr, slot)

	cs := makeChangeSet(key, padLeft32(0x56), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	v := commitAndCheck(t, s)
	require.Equal(t, int64(1), v)

	// LocalMeta should be updated
	require.Equal(t, int64(1), s.localMeta[storageDBDir].CommittedVersion)

	// Verify it's persisted in DB
	requireFlushedToDisk(t, s)
	data, err := s.rawDBFor(storageDBDir).Get(ktype.MetaVersionKey)
	require.NoError(t, err)
	require.Equal(t, int64(1), int64(binary.BigEndian.Uint64(data)))
}

// =============================================================================
// SetInitialVersion
// =============================================================================

func TestSetInitialVersion_HappyPath(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	require.NoError(t, s.SetInitialVersion(100))
	require.Equal(t, int64(99), s.committedVersion)
	target, err := os.Readlink(currentPath(s.flatkvDir()))
	require.NoError(t, err)
	require.Equal(t, snapshotName(99), target)

	addr := ktype.Address{0xAA}
	slot := ktype.Slot{0xBB}
	cs := makeChangeSet(evmStorageKey(addr, slot), padLeft32(0xCC), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))

	v, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)
	require.Equal(t, int64(100), v, "first Commit after SetInitialVersion(100) must produce version 100")
	require.Equal(t, int64(100), s.Version())
}

func TestSetInitialVersion_GenesisSkipsSeededSnapshot(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	require.NoError(t, s.SetInitialVersion(1))
	require.Equal(t, int64(0), s.committedVersion)
	target, err := os.Readlink(currentPath(s.flatkvDir()))
	require.NoError(t, err)
	require.Equal(t, snapshotName(0), target)

	addr := ktype.Address{0xAA}
	slot := ktype.Slot{0xBB}
	cs := makeChangeSet(evmStorageKey(addr, slot), padLeft32(0xCC), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))

	v, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), v, "first Commit after SetInitialVersion(1) must produce version 1")
}

func TestSetInitialVersion_RejectsAfterCommit(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0x01}
	slot := ktype.Slot{0x02}
	cs := makeChangeSet(evmStorageKey(addr, slot), padLeft32(0x03), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	_, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)

	err = s.SetInitialVersion(50)
	require.Error(t, err)
	require.Contains(t, err.Error(), "fresh store")
}

func TestSetInitialVersion_RejectsReadOnly(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0x01}
	slot := ktype.Slot{0x02}
	cs := makeChangeSet(evmStorageKey(addr, slot), padLeft32(0x03), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	_, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)

	roStore, err := s.LoadVersionReadOnly(0)
	require.NoError(t, err)
	defer roStore.Close()

	err = roStore.SetInitialVersion(50)
	require.Error(t, err)
	require.ErrorIs(t, err, errReadOnly)
}

func TestSetInitialVersion_RejectsNonPositive(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	require.Error(t, s.SetInitialVersion(0))
	require.Error(t, s.SetInitialVersion(-1))
	require.Equal(t, int64(0), s.committedVersion, "rejected calls must not mutate state")
}

func TestSetInitialVersion_SurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultConfig()
	cfg.DataDir = dbDir
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	require.NoError(t, s.SetInitialVersion(100))
	require.NoError(t, s.Close())

	cfg2 := config.DefaultConfig()
	cfg2.DataDir = dbDir
	s2, err := newCommitStoreWithWAL(context.Background(), cfg2)
	require.NoError(t, err)
	err = s2.LoadLatest()
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(99), s2.committedVersion,
		"persisted committedVersion must equal initialVersion-1 after reopen")

	addr := ktype.Address{0xDD}
	slot := ktype.Slot{0xEE}
	cs := makeChangeSet(evmStorageKey(addr, slot), padLeft32(0xFF), false)
	require.NoError(t, s2.ApplyChangeSets(s2.Version()+1, []*proto.NamedChangeSet{cs}))
	v, err := s2.Commit(s2.Version() + 1)
	require.NoError(t, err)
	require.Equal(t, int64(100), v,
		"first Commit after reopen must produce initialVersion")
}

func TestSetInitialVersion_RollbackBelowSeededVersionFails(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	require.NoError(t, s.SetInitialVersion(100))

	addr := ktype.Address{0x77}
	slot := ktype.Slot{0x88}
	cs := makeChangeSet(evmStorageKey(addr, slot), padLeft32(0x01), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	_, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)
	require.Equal(t, int64(100), s.Version())

	err = s.Rollback(50)
	require.Error(t, err,
		"rollback below initialVersion-1 must fail; nothing exists before the seeded baseline")
}

// =============================================================================
// Derived Global State After Commit + Reopen
// =============================================================================

func TestDerivedGlobalStatePersistence(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultConfig()
	cfg.DataDir = dbDir
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})
	commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0xBB})

	// The store keeps no global record: its version is the minimum of the data
	// DBs' own version records and its root is the sum of their roots. Read both
	// off disk and check the derivation rather than a stored copy.
	derived := lthash.New()
	for _, ndb := range selectDataDBs(t, s, nil) {
		meta, err := loadLocalMeta(ndb.db)
		require.NoError(t, err)
		require.Equal(t, int64(2), meta.CommittedVersion, "%s version record", ndb.dir)
		derived.MixIn(meta.LtHash)
	}
	require.Equal(t, s.committedLtHash.Checksum(), derived.Checksum())

	expectedHash := s.committedLtHash.Checksum()
	require.NoError(t, s.Close())

	cfg2 := config.DefaultConfig()
	cfg2.DataDir = dbDir
	s2, err := newCommitStoreWithWAL(context.Background(), cfg2)
	require.NoError(t, err)
	err = s2.LoadLatest()
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(2), s2.committedVersion)
	require.Equal(t, expectedHash, s2.committedLtHash.Checksum(),
		"global LtHash should survive reopen")
}

// =============================================================================
// GetLatestVersion (free-standing helper + method)
// =============================================================================

func TestGetLatestVersionFreshDirReturnsZero(t *testing.T) {
	dir := t.TempDir()
	v, err := GetLatestVersion(filepath.Join(dir, flatkvRootDir))
	require.NoError(t, err)
	require.Equal(t, int64(0), v,
		"never-opened flatkv dir must report version 0, not an error")
}

func TestGetLatestVersionAfterCommitsReadsWorkingMisc(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultConfig()
	cfg.DataDir = dbDir
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})
	commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0xBB})
	commitStorageEntry(t, s, ktype.Address{0x03}, ktype.Slot{0x03}, []byte{0xCC})

	require.NoError(t, s.Close())

	v, err := GetLatestVersion(dbDir)
	require.NoError(t, err)
	require.Equal(t, int64(3), v,
		"helper must read MetaVersionKey from working/misc after a clean close")
}

func TestGetLatestVersionMissingKeyReturnsZero(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultConfig()
	cfg.DataDir = dbDir
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)
	require.NoError(t, s.Close())

	v, err := GetLatestVersion(dbDir)
	require.NoError(t, err)
	require.Equal(t, int64(0), v,
		"opened-then-closed-with-no-commits flatkv must report version 0")
}

func TestCommitStoreGetLatestVersionReturnsInMemoryWhenLoaded(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	v, err := s.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(0), v)

	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})
	commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0xBB})

	v, err = s.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(2), v,
		"method on an open store must return the in-memory committed version")
}

func TestCommitStoreGetLatestVersionFallsBackToDiskWhenUnloaded(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultConfig()
	cfg.DataDir = dbDir
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)
	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})
	commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0xBB})
	require.NoError(t, s.Close())

	cfg2 := config.DefaultConfig()
	cfg2.DataDir = dbDir
	s2, err := newCommitStoreWithWAL(context.Background(), cfg2)
	require.NoError(t, err)
	defer s2.Close()

	v, err := s2.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(2), v,
		"method on a not-yet-opened store must fall through to the on-disk helper")
}

// =============================================================================
// Data DB alignment
// =============================================================================

// TestDataDBAheadOfWALIsRebuiltNotRefused pins the repair for a data DB left above the WAL tail. It
// holds a block that is in no other DB and in no snapshot, so no consistent state includes it and
// nothing can be served from it. The store rebuilds its working copy from the snapshot and replays
// forward rather than refusing to open, because refusing would take a node down until an operator
// performed by hand the very deletion the rebuild performs.
//
// This is the variant where the replayed blocks do not touch the ahead DB. The variant where they do —
// which is what let replay rewrite the DB's record downward and hide the problem — is covered by
// TestDataDBAheadOfWALTouchedByReplayIsRebuilt.
func TestDataDBAheadOfWALIsRebuiltNotRefused(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dbDir
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})
	commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0xBB})

	// Push accountDB one block past the WAL tail. No replay can carry it there.
	rewindVersionRecords(t, s, 3, accountDBDir)
	require.NoError(t, s.Close())

	cfg2 := config.DefaultTestConfig(t)
	cfg2.DataDir = dbDir
	s2, err := newCommitStoreWithWAL(t.Context(), cfg2)
	require.NoError(t, err)
	defer s2.Close()

	require.NoError(t, s2.LoadLatest(), "an unreachable version must be repaired, not fatal")
	require.Equal(t, int64(2), s2.Version(), "the store lands at the WAL tail")
	for _, ndb := range selectDataDBs(t, s2, nil) {
		meta, err := loadLocalMeta(ndb.db)
		require.NoError(t, err)
		require.Equal(t, int64(2), meta.CommittedVersion, "%s version record", ndb.dir)
	}
	require.NoError(t, VerifyLtHash(s2), "the store root must describe what the DBs actually hold")
}

// TestEmptyBlockAdvancesWatermarkAcrossReopen pins that a block touching no data
// DB still moves every DB's version record. The store's watermark is the minimum
// of those records, so a DB that skipped the block would hold the whole store
// back and force the next open to replay from there.
func TestEmptyBlockAdvancesWatermarkAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dbDir
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())

	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})
	_, err = s.Commit(s.Version() + 1) // block 2: no ApplyChangeSets at all
	require.NoError(t, err)
	require.Equal(t, int64(2), s.Version())

	for _, ndb := range selectDataDBs(t, s, nil) {
		meta, err := loadLocalMeta(ndb.db)
		require.NoError(t, err)
		require.Equal(t, int64(2), meta.CommittedVersion,
			"%s must record the empty block, not stay behind at 1", ndb.dir)
	}
	require.NoError(t, s.Close())

	v, err := GetLatestVersion(dbDir)
	require.NoError(t, err)
	require.Equal(t, int64(2), v, "the empty block must be durable, not replayed again")
}
