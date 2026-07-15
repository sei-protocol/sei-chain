package composite

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// failingEVMStore is a mock flatkv.Store whose LoadVersion always fails.
type failingEVMStore struct{}

var _ flatkv.Store = (*failingEVMStore)(nil)

func (f *failingEVMStore) LoadVersion(int64, bool) (flatkv.Store, error) {
	return nil, fmt.Errorf("flatkv unavailable")
}
func (f *failingEVMStore) ApplyChangeSets([]*proto.NamedChangeSet) error { return nil }
func (f *failingEVMStore) Commit() (int64, error)                        { return 0, nil }
func (f *failingEVMStore) SetInitialVersion(int64) error                 { return nil }
func (f *failingEVMStore) Get(string, []byte) ([]byte, bool)             { return nil, false }
func (f *failingEVMStore) GetBlockHeightModified(string, []byte) (int64, bool, error) {
	return -1, false, nil
}
func (f *failingEVMStore) Has(string, []byte) bool                  { return false }
func (f *failingEVMStore) RawGlobalIterator() (dbm.Iterator, error) { return nil, nil }
func (f *failingEVMStore) Iterator(string, []byte, []byte, bool) (dbm.Iterator, error) {
	return nil, nil
}
func (f *failingEVMStore) RootHash() []byte                              { return nil }
func (f *failingEVMStore) Version() int64                                { return 0 }
func (f *failingEVMStore) EarliestVersion() int64                        { return 0 }
func (f *failingEVMStore) GetLatestVersion() (int64, error)              { return 0, nil }
func (f *failingEVMStore) WriteSnapshot(string) error                    { return nil }
func (f *failingEVMStore) Rollback(int64) error                          { return nil }
func (f *failingEVMStore) Exporter(int64) (types.Exporter, error)        { return nil, nil }
func (f *failingEVMStore) Importer(int64) (types.Importer, error)        { return nil, nil }
func (f *failingEVMStore) GetPhaseTimer() *metrics.PhaseTimer            { return nil }
func (f *failingEVMStore) CommittedRootHash() []byte                     { return nil }
func (f *failingEVMStore) HashCategories() []string                      { return nil }
func (f *failingEVMStore) RecordHashes(hashlog.HashLogger, uint64) error { return nil }
func (f *failingEVMStore) CleanupOrphanedReadOnlyDirs() error            { return nil }
func (f *failingEVMStore) Close() error                                  { return nil }

// eraFailingEVMStore is a failingEVMStore with a configurable
// EarliestVersion, used to exercise Exporter's pre-era vs in-history
// classification of a flatkv load failure.
type eraFailingEVMStore struct {
	failingEVMStore
	earliest int64
}

var _ flatkv.Store = (*eraFailingEVMStore)(nil)

func (f *eraFailingEVMStore) EarliestVersion() int64 { return f.earliest }

func padLeft32(val ...byte) []byte {
	var b [32]byte
	copy(b[32-len(val):], val)
	return b[:]
}

func TestCompositeStoreBasicOperations(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))

	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, cs.Close())
	}()

	require.Equal(t, int64(0), cs.Version())

	// Apply changesets with both regular and EVM data
	changesets := []*proto.NamedChangeSet{
		{
			Name: keys.BankStoreKey,
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: []byte("key1"), Value: []byte("value1")},
				},
			},
		},
		{
			Name: keys.EVMStoreKey,
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: []byte("evm_key1"), Value: []byte("evm_value1")},
				},
			},
		},
	}
	err = cs.ApplyChangeSets(changesets)
	require.NoError(t, err)

	version, err := cs.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), version)
	require.Equal(t, int64(1), cs.Version())

	testStore := cs.GetChildStoreByName(keys.BankStoreKey)
	require.NotNil(t, testStore)

	evmStore := cs.GetChildStoreByName(keys.EVMStoreKey)
	require.NotNil(t, evmStore)
}

func TestEmptyChangesets(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey}))

	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, cs.Close())
	}()

	// Empty changesets should be no-op
	err = cs.ApplyChangeSets(nil)
	require.NoError(t, err)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{})
	require.NoError(t, err)
}

func TestLoadVersionCopyExisting(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey}))

	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: keys.BankStoreKey,
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: []byte("key"), Value: []byte("value")},
				},
			},
		},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)
	require.NoError(t, cs.Close())

	// Load with copyExisting=true
	newCS, err := cs.LoadVersion(0, true)
	require.NoError(t, err)
	require.NotNil(t, newCS)

	compositeCS, ok := newCS.(*CompositeCommitStore)
	require.True(t, ok)
	require.NotSame(t, cs, compositeCS)

	require.NoError(t, compositeCS.Close())
}

func TestWorkingAndLastCommitInfo(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey}))

	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, cs.Close())
	}()

	workingInfo := cs.WorkingCommitInfo()
	require.NotNil(t, workingInfo)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: keys.BankStoreKey,
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: []byte("key"), Value: []byte("value")},
				},
			},
		},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)

	lastInfo := cs.LastCommitInfo()
	require.NotNil(t, lastInfo)
	require.Equal(t, int64(1), lastInfo.Version)
}

func TestLatticeHashCommitInfo(t *testing.T) {
	addr := [20]byte{0xAA}
	slot := [32]byte{0xBB}
	evmStorageKey := keys.BuildEVMKey(keys.EVMKeyStorage, append(addr[:], slot[:]...))

	makeChangesets := func(round byte) []*proto.NamedChangeSet {
		return []*proto.NamedChangeSet{
			{
				Name: keys.BankStoreKey,
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
						{Key: []byte("key"), Value: []byte{round}},
					},
				},
			},
			{
				Name: keys.EVMStoreKey,
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
						{Key: evmStorageKey, Value: padLeft32(round)},
					},
				},
			},
		}
	}

	tests := []struct {
		name          string
		writeMode     types.WriteMode
		expectLattice bool
	}{
		{"MemiavlOnly", types.MemiavlOnly, false},
		{"TestOnlyDualWrite", types.TestOnlyDualWrite, true},
		{"EVMMigrated", types.EVMMigrated, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := config.DefaultStateCommitConfig()
			cfg.WriteMode = tt.writeMode

			cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
			require.NoError(t, err)
			require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
			_, err = cs.LoadVersion(0, false)
			require.NoError(t, err)
			defer cs.Close()

			var prevLastHash []byte

			for round := byte(1); round <= 3; round++ {
				require.NoError(t, cs.ApplyChangeSets(makeChangesets(round)))

				// --- Working commit info ---
				expectedCosmos := cs.memIAVL.WorkingCommitInfo()
				var expectedEvmHash []byte
				if tt.expectLattice {
					expectedEvmHash = cs.flatKV.RootHash()
				}

				workingInfo := cs.WorkingCommitInfo()
				cosmosCount := len(expectedCosmos.StoreInfos)
				if tt.expectLattice {
					require.Equal(t, cosmosCount+1, len(workingInfo.StoreInfos))
				} else {
					require.Equal(t, cosmosCount, len(workingInfo.StoreInfos))
				}
				for i, si := range expectedCosmos.StoreInfos {
					require.Equal(t, si.Name, workingInfo.StoreInfos[i].Name)
					require.Equal(t, si.CommitId.Hash, workingInfo.StoreInfos[i].CommitId.Hash)
				}
				if tt.expectLattice {
					entry := workingInfo.StoreInfos[len(workingInfo.StoreInfos)-1]
					require.Equal(t, "evm_lattice", entry.Name)
					require.Equal(t, expectedEvmHash, entry.CommitId.Hash)
					require.Equal(t, workingInfo.Version, entry.CommitId.Version)

					// Verify no duplicate names — important for app hash merkle tree
					names := make(map[string]int)
					for _, si := range workingInfo.StoreInfos {
						names[si.Name]++
					}
					for name, count := range names {
						require.Equal(t, 1, count, "duplicate store name %q in WorkingCommitInfo", name)
					}
				}

				// --- Commit ---
				_, err = cs.Commit()
				require.NoError(t, err)

				// --- Last commit info ---
				expectedCosmosLast := cs.memIAVL.LastCommitInfo()
				var expectedEvmCommitted []byte
				if tt.expectLattice {
					expectedEvmCommitted = cs.flatKV.CommittedRootHash()
					require.Equal(t, expectedEvmHash, expectedEvmCommitted)
				}

				lastInfo := cs.LastCommitInfo()
				require.Equal(t, int64(round), lastInfo.Version)
				cosmosLastCount := len(expectedCosmosLast.StoreInfos)
				if tt.expectLattice {
					require.Equal(t, cosmosLastCount+1, len(lastInfo.StoreInfos))
				} else {
					require.Equal(t, cosmosLastCount, len(lastInfo.StoreInfos))
				}
				for i, si := range expectedCosmosLast.StoreInfos {
					require.Equal(t, si.Name, lastInfo.StoreInfos[i].Name)
					require.Equal(t, si.CommitId.Hash, lastInfo.StoreInfos[i].CommitId.Hash)
				}
				if tt.expectLattice {
					entry := lastInfo.StoreInfos[len(lastInfo.StoreInfos)-1]
					require.Equal(t, "evm_lattice", entry.Name)
					require.Equal(t, expectedEvmCommitted, entry.CommitId.Hash)
					require.Equal(t, lastInfo.Version, entry.CommitId.Version)

					// Verify no duplicate names — important for app hash merkle tree
					names := make(map[string]int)
					for _, si := range lastInfo.StoreInfos {
						names[si.Name]++
					}
					for name, count := range names {
						require.Equal(t, 1, count, "duplicate store name %q in LastCommitInfo", name)
					}

					// Hash must change between rounds since data differs
					if prevLastHash != nil {
						require.NotEqual(t, prevLastHash, entry.CommitId.Hash,
							"lattice hash should change across commits")
					}
					prevLastHash = entry.CommitId.Hash
				}
			}
		})
	}
}

// containsLatticeStoreInfo reports whether the given StoreInfos contains an
// entry named "evm_lattice".
func containsLatticeStoreInfo(infos []proto.StoreInfo) bool {
	for _, si := range infos {
		if si.Name == "evm_lattice" {
			return true
		}
	}
	return false
}

// cloneStoreInfos returns a deep copy of the given StoreInfos, suitable for
// comparing values captured before and after a Close()/reopen() cycle where
// the underlying memiavl buffers would otherwise be reused.
func cloneStoreInfos(infos []proto.StoreInfo) []proto.StoreInfo {
	out := make([]proto.StoreInfo, len(infos))
	for i, si := range infos {
		hashCopy := make([]byte, len(si.CommitId.Hash))
		copy(hashCopy, si.CommitId.Hash)
		out[i] = proto.StoreInfo{
			Name: si.Name,
			CommitId: proto.CommitID{
				Version: si.CommitId.Version,
				Hash:    hashCopy,
			},
		}
	}
	return out
}

// TestMemiavlOnlyToMigrateEVMPreservesLastCommitInfoBeforeFirstCommit pins the
// AppHash-continuity invariant for a live migration from MemiavlOnly to
// MigrateEVM. After running a chain for N blocks under MemiavlOnly and then
// restarting under MigrateEVM, LastCommitInfo() must report the same store
// set at the same hashes as before the restart, until the first
// post-restart commit actually advances the migration boundary. If
// LastCommitInfo gains an evm_lattice entry on bare restart, the merkle
// root over StoreInfos changes at an already-committed height and the
// Tendermint handshake fails.
//
// Today this test fails because LastCommitInfo() appends evm_lattice
// whenever flatKV != nil, with no gating on the migration boundary.
func TestMemiavlOnlyToMigrateEVMPreservesLastCommitInfoBeforeFirstCommit(t *testing.T) {
	dir := t.TempDir()

	// Phase 1: run for several blocks under MemiavlOnly, exercising both
	// bank/ and evm/ stores so the captured StoreInfos contain non-trivial
	// hashes for every module the post-restart composite will report.
	cosmosCfg := config.DefaultStateCommitConfig()
	cosmosCfg.WriteMode = types.MemiavlOnly

	cs1, err := NewCompositeCommitStore(t.Context(), dir, cosmosCfg)
	require.NoError(t, err)
	require.NoError(t, cs1.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs1.LoadVersion(0, false)
	require.NoError(t, err)
	require.Nil(t, cs1.flatKV, "MemiavlOnly must not allocate a flatkv store")

	const phase1Blocks = 10
	for i := 0; i < phase1Blocks; i++ {
		require.NoError(t, cs1.ApplyChangeSets([]*proto.NamedChangeSet{
			{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte(fmt.Sprintf("bank_%d", i)), Value: []byte{byte(i)}},
			}}},
			{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte(fmt.Sprintf("evm_%d", i)), Value: []byte{byte(i)}},
			}}},
		}))
		_, err := cs1.Commit()
		require.NoError(t, err)
	}

	preInfo := cs1.LastCommitInfo()
	require.NotNil(t, preInfo)
	preVersion := preInfo.Version
	preStoreInfos := cloneStoreInfos(preInfo.StoreInfos)
	require.False(t, containsLatticeStoreInfo(preStoreInfos),
		"MemiavlOnly LastCommitInfo must not contain evm_lattice (precondition)")
	require.NoError(t, cs1.Close())

	// Phase 2: reopen the same data directory with MigrateEVM. The
	// LoadVersion path seeds flatkv into lockstep with memiavl, but no
	// changesets have been applied yet, so the migration boundary on
	// disk is absent (NotStarted). The composite must therefore report
	// the same LastCommitInfo as the MemiavlOnly run did at the same
	// height.
	migrateCfg := config.DefaultStateCommitConfig()
	migrateCfg.WriteMode = types.MigrateEVM
	cs2, err := NewCompositeCommitStore(t.Context(), dir, migrateCfg)
	require.NoError(t, err)
	require.NoError(t, cs2.SetMigrationBatchSize(100))
	require.NoError(t, cs2.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs2.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs2.Close()

	require.NotNil(t, cs2.flatKV, "MigrateEVM must allocate a flatkv store")
	require.Equal(t, int64(phase1Blocks), cs2.Version(),
		"composite version must survive reopen unchanged")

	postInfo := cs2.LastCommitInfo()
	require.NotNil(t, postInfo)
	require.Equal(t, preVersion, postInfo.Version,
		"LastCommitInfo.Version must not change across a bare restart into MigrateEVM")
	require.False(t, containsLatticeStoreInfo(postInfo.StoreInfos),
		"LastCommitInfo must not contain evm_lattice on a bare restart into MigrateEVM "+
			"before any post-restart commit (the migration boundary is still NotStarted)")
	require.Equal(t, len(preStoreInfos), len(postInfo.StoreInfos),
		"LastCommitInfo.StoreInfos length must match the pre-restart value (no extra evm_lattice entry)")
	for i, pre := range preStoreInfos {
		post := postInfo.StoreInfos[i]
		require.Equal(t, pre.Name, post.Name, "StoreInfos[%d].Name must match", i)
		require.Equal(t, pre.CommitId.Version, post.CommitId.Version,
			"StoreInfos[%d].CommitId.Version must match for store %q", i, pre.Name)
		require.Equal(t, pre.CommitId.Hash, post.CommitId.Hash,
			"StoreInfos[%d].CommitId.Hash must match for store %q (AppHash continuity)", i, pre.Name)
	}
}

// TestMigrateEVMGenesisPreFirstCommitOmitsLatticeHash exercises the same
// gating rule from the genesis side: a fresh chain configured with
// MigrateEVM but with no committed blocks yet must report a LastCommitInfo
// without evm_lattice (the migration boundary on disk is absent, i.e.
// NotStarted). The on-disk AppHash for that genesis height is what
// Tendermint will persist for height 0, so it must not depend on the
// presence of a not-yet-started migration backend.
//
// Today this test fails because LastCommitInfo() always appends
// evm_lattice when flatKV != nil.
func TestMigrateEVMGenesisPreFirstCommitOmitsLatticeHash(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MigrateEVM
	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	require.NoError(t, cs.SetMigrationBatchSize(100))
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs.Close()

	require.NotNil(t, cs.flatKV, "MigrateEVM must allocate a flatkv store")
	require.Equal(t, int64(0), cs.Version(), "fresh MigrateEVM store must report version 0")

	info := cs.LastCommitInfo()
	require.NotNil(t, info)
	require.False(t, containsLatticeStoreInfo(info.StoreInfos),
		"MigrateEVM LastCommitInfo before any commit must not contain evm_lattice "+
			"(the migration boundary is NotStarted)")

	working := cs.WorkingCommitInfo()
	require.NotNil(t, working)
	require.False(t, containsLatticeStoreInfo(working.StoreInfos),
		"MigrateEVM WorkingCommitInfo before any commit must not contain evm_lattice")
}

// TestMigrateEVMIncludesLatticeHashAfterFirstCommit is the positive
// counterpart to the two gating tests above. Once the first ApplyChangeSets
// has run under MigrateEVM, the migration manager advances the boundary
// (and persists it to flatkv), so the lattice must immediately appear in
// LastCommitInfo. Tendermint will accept any subsequent restart at this
// height because the persisted AppHash for the just-committed block
// already accounts for the lattice contribution.
//
// This test passes today (vacuously, since the current code always
// appends) and must continue to pass after the fix.
func TestMigrateEVMIncludesLatticeHashAfterFirstCommit(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MigrateEVM
	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	require.NoError(t, cs.SetMigrationBatchSize(100))
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs.Close()

	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k"), Value: []byte("v")},
		}}},
	}))
	_, err = cs.Commit()
	require.NoError(t, err)

	info := cs.LastCommitInfo()
	require.NotNil(t, info)
	require.True(t, containsLatticeStoreInfo(info.StoreInfos),
		"MigrateEVM LastCommitInfo after the first commit must contain evm_lattice "+
			"(the migration boundary has advanced past NotStarted)")
}

// TestMigrateEVMLatticeRemainsAfterRestartPostMigrationCompletion verifies
// that the gating rule does not regress past the completion block. On the
// completion block the MigrationManager atomically deletes
// MigrationBoundaryKey and writes MigrationVersionKey to flatkv. On a
// subsequent restart in MigrateEVM, only the version key remains on disk;
// the boundary key is gone. The lattice must still be included in
// LastCommitInfo at that height because the stored AppHash for the
// completion block already accounted for it.
//
// A naive fix that only consults MigrationBoundaryKey would fail this
// test by suppressing the lattice after completion. The intended
// implementation must also consult MigrationVersionKey (or another
// post-completion signal).
//
// This test passes today (vacuously) and must continue to pass after
// the fix.
func TestMigrateEVMLatticeRemainsAfterRestartPostMigrationCompletion(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MigrateEVM
	// A large batch size ensures the migration completes in a single
	// ApplyChangeSets call: there are no pre-existing evm/ keys, so the
	// iterator's first batch reports MigrationBoundaryComplete and the
	// manager atomically deletes the boundary key and writes the version
	// key on the same commit.
	cs1, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs1.SetMigrationBatchSize(1000))
	require.NoError(t, cs1.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs1.LoadVersion(0, false)
	require.NoError(t, err)

	require.NoError(t, cs1.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k"), Value: []byte("v")},
		}}},
	}))
	_, err = cs1.Commit()
	require.NoError(t, err)

	// Confirm on-disk MigrationStore reflects a completed migration:
	// MigrationVersionKey is set, MigrationBoundaryKey is absent.
	versionBytes, versionPresent := cs1.flatKV.Get(migration.MigrationStore, []byte(migration.MigrationVersionKey))
	require.True(t, versionPresent,
		"MigrationVersionKey must be set on the flatkv MigrationStore after the completion block")
	require.NotEmpty(t, versionBytes)
	_, boundaryPresent := cs1.flatKV.Get(migration.MigrationStore, []byte(migration.MigrationBoundaryKey))
	require.False(t, boundaryPresent,
		"MigrationBoundaryKey must be absent on the flatkv MigrationStore after the completion block")

	require.True(t, containsLatticeStoreInfo(cs1.LastCommitInfo().StoreInfos),
		"LastCommitInfo at the completion block must contain evm_lattice")
	require.NoError(t, cs1.Close())

	// Reopen in MigrateEVM. The boundary key is gone, so any rule that
	// only inspects MigrationBoundaryKey would treat this state as
	// NotStarted and wrongly suppress the lattice — silently rewriting
	// the AppHash that Tendermint already accepted at this height.
	cs2, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs2.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs2.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs2.Close()

	require.True(t, containsLatticeStoreInfo(cs2.LastCommitInfo().StoreInfos),
		"LastCommitInfo after restart at a post-completion height must still contain evm_lattice "+
			"(MigrationVersionKey on disk indicates the migration is done)")
}

func TestRollback(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey}))

	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Commit a few versions
	for i := 0; i < 3; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: keys.BankStoreKey,
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
						{Key: []byte("key"), Value: []byte("value" + string(rune('0'+i)))},
					},
				},
			},
		})
		require.NoError(t, err)
		_, err = cs.Commit()
		require.NoError(t, err)
	}

	require.Equal(t, int64(3), cs.Version())

	err = cs.Rollback(2)
	require.NoError(t, err)
	require.Equal(t, int64(2), cs.Version())

	require.NoError(t, cs.Close())
}

func TestGetVersions(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey}))

	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: keys.BankStoreKey,
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
						{Key: []byte("key"), Value: []byte("value")},
					},
				},
			},
		})
		require.NoError(t, err)
		_, err = cs.Commit()
		require.NoError(t, err)
	}
	require.NoError(t, cs.Close())

	cs2, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs2.Initialize([]string{keys.BankStoreKey}))

	latestVersion, err := cs2.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(3), latestVersion)
}

// TestGetLatestVersionMemiavlOnly verifies the routing path for
// MemiavlOnly: the answer comes from memiavl and flatkv is not
// consulted (it is nil).
func TestGetLatestVersionMemiavlOnly(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MemiavlOnly
	// memiavl.GetLatestVersion reads the on-disk WAL tail; with the
	// default async buffer wal.Write returns before the entry is
	// durable, which races with the read below. Force synchronous
	// WAL writes so by the time Commit returns the disk reflects
	// the new version. See the doc comment on
	// CompositeCommitStore.GetLatestVersion for the full rationale.
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()
	require.Nil(t, cs.flatKV, "MemiavlOnly must not allocate flatKV")

	for i := 0; i < 2; i++ {
		require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("k"), Value: []byte("v")},
			}}},
		}))
		_, err = cs.Commit()
		require.NoError(t, err)
	}

	v, err := cs.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(2), v,
		"MemiavlOnly must route GetLatestVersion to memiavl without consulting flatkv")
}

// TestGetLatestVersionFlatKVOnly verifies the routing path for
// FlatKVOnly: the answer comes from flatkv and memiavl is not
// consulted (it is nil).
func TestGetLatestVersionFlatKVOnly(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.FlatKVOnly

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()
	require.Nil(t, cs.memIAVL, "FlatKVOnly must not allocate memIAVL")

	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k1"), Value: []byte("v1")},
		}}},
	}))
	_, err = cs.Commit()
	require.NoError(t, err)

	v, err := cs.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(1), v,
		"FlatKVOnly must route GetLatestVersion to flatkv without nil-deref of memiavl")
}

// TestGetLatestVersionBothBackendsAligned verifies that with both
// backends configured and in lockstep, GetLatestVersion returns the
// common value without error.
func TestGetLatestVersionBothBackendsAligned(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MigrateEVM
	// Force synchronous memiavl WAL writes so the on-disk tail
	// reflects every Commit before GetLatestVersion reads it (the
	// flatkv side is already synchronous). See the doc comment on
	// CompositeCommitStore.GetLatestVersion for the full rationale.
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.SetMigrationBatchSize(100))
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()
	require.NotNil(t, cs.memIAVL)
	require.NotNil(t, cs.flatKV)

	for i := 0; i < 3; i++ {
		require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("k"), Value: []byte("v")},
			}}},
		}))
		_, err = cs.Commit()
		require.NoError(t, err)
	}

	v, err := cs.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(3), v,
		"aligned backends must produce a single agreed-upon version")
}

// TestReadOnlyLoadVersionFailsLoudWhenFlatKVUnavailable verifies the
// post-section-4 fail-loud contract: when a non-MemiavlOnly composite
// store is loaded read-only and the flatkv backend fails to load, the
// load itself returns an error rather than silently dropping flatkv
// (the prior soft-fail behavior). Recovering from DB errors is the
// caller's responsibility a layer up.
func TestReadOnlyLoadVersionFailsLoudWhenFlatKVUnavailable(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0
	// Need flatkv to be allocated and exercised by LoadVersion;
	// MemiavlOnly would not touch the flatkv path at all.
	cfg.WriteMode = types.MigrateEVM
	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.SetMigrationBatchSize(100))
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))

	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: keys.BankStoreKey,
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: []byte("key1"), Value: []byte("value1")},
				},
			},
		},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)

	// Inject a failing EVM committer. The read-only load must surface
	// the error rather than swallow it.
	cs.flatKV = &failingEVMStore{}

	_, err = cs.LoadVersion(0, true)
	require.Error(t, err, "readonly LoadVersion must fail loud when FlatKV is unavailable")
	require.Contains(t, err.Error(), "FlatKV")
}

// TestLoadVersionFlatKVOnlyReadWrite verifies the writable read path
// in FlatKVOnly mode: memIAVL is nil, only flatkv is opened, and the
// router is built against flatkv alone. Writes and reads round-trip
// through the router without nil-dereferencing memIAVL (Problem 1 of
// the section 4 LoadVersion rewrite).
func TestLoadVersionFlatKVOnlyReadWrite(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.FlatKVOnly

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	require.Nil(t, cs.memIAVL, "FlatKVOnly must not allocate memIAVL")
	require.NotNil(t, cs.flatKV, "FlatKVOnly must allocate flatKV")

	committer, err := cs.LoadVersion(0, false)
	require.NoError(t, err, "LoadVersion must not nil-deref memIAVL in FlatKVOnly")
	defer func() { _ = cs.Close() }()
	require.Same(t, cs, committer, "writable LoadVersion returns the receiver")
	require.NotNil(t, cs.router, "router must be built after LoadVersion")

	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k1"), Value: []byte("v1")},
		}}},
	}))
	_, err = cs.Commit()
	require.NoError(t, err)

	got, ok, err := cs.Get(keys.EVMStoreKey, []byte("k1"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("v1"), got)
}

// TestLoadVersionFlatKVOnlyReadOnly verifies the read-only handle
// returned by LoadVersion(_, true) in FlatKVOnly mode is fully usable:
// it has its own router (Problem 3 of section 4) and sees the data
// committed on the writable handle.
func TestLoadVersionFlatKVOnlyReadOnly(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.FlatKVOnly

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()

	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k1"), Value: []byte("v1")},
		}}},
	}))
	_, err = cs.Commit()
	require.NoError(t, err)

	ro, err := cs.LoadVersion(0, true)
	require.NoError(t, err)
	defer func() { _ = ro.Close() }()
	roComposite, ok := ro.(*CompositeCommitStore)
	require.True(t, ok)
	require.Nil(t, roComposite.memIAVL, "FlatKVOnly read-only must not have memIAVL")
	require.NotNil(t, roComposite.router, "read-only handle must have its own router")

	got, ok, err := roComposite.Get(keys.EVMStoreKey, []byte("k1"))
	require.NoError(t, err, "read-only handle must serve reads without nil-dereferencing router")
	require.True(t, ok)
	require.Equal(t, []byte("v1"), got)
}

// TestLoadVersionRebuildsRouterOnReload verifies that calling
// LoadVersion a second time on the same store builds a fresh router
// and cancels the previous router's context. The cancel is observable
// via the routerCancel field: the second LoadVersion must replace it
// with a new function, and the first one must report Cancelled when
// invoked indirectly through the context the buildRouter handed to
// BuildRouter.
func TestLoadVersionRebuildsRouterOnReload(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MigrateEVM
	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	require.NoError(t, cs.SetMigrationBatchSize(100))
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))

	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	firstRouter := cs.router
	firstCancel := cs.routerCancel
	require.NotNil(t, firstRouter)
	require.NotNil(t, firstCancel)

	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	require.NotNil(t, cs.router)
	require.NotSame(t, firstRouter, cs.router, "LoadVersion must rebuild the router")
	require.NotNil(t, cs.routerCancel)

	require.NoError(t, cs.Close())
	require.Nil(t, cs.routerCancel, "Close must clear routerCancel")
	require.Nil(t, cs.router, "Close must clear router")
}

// TestLoadVersionDoesNotMountMigrationStoreInMigrationMode pins the
// post-cleanup contract: opening in a migration mode must NOT
// materialize a "migration" tree on memiavl. All migration metadata
// (version, boundary) is owned exclusively by flatkv. Mounting an
// empty memiavl tree would silently widen CommitInfo.StoreInfos and
// change the app hash.
func TestLoadVersionDoesNotMountMigrationStoreInMigrationMode(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MigrateEVM
	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	require.NoError(t, cs.SetMigrationBatchSize(100))
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err, "LoadVersion in migration mode must succeed without mounting a migration tree on memiavl")
	defer func() { _ = cs.Close() }()

	require.Nil(t, cs.memIAVL.GetChildStoreByName(migration.MigrationStore),
		"migration mode must not mount a migration tree on memiavl")
	for _, si := range cs.WorkingCommitInfo().StoreInfos {
		require.NotEqual(t, migration.MigrationStore, si.Name,
			"WorkingCommitInfo must not contain a migration StoreInfo on memiavl")
	}
}

// TestLoadVersionDoesNotMountMigrationStoreInMemiavlOnly pins the same
// contract for non-migration modes. This was already correct; keep
// asserting it so the negative case stays in CI.
func TestLoadVersionDoesNotMountMigrationStoreInMemiavlOnly(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MemiavlOnly

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()

	require.Nil(t, cs.memIAVL.GetChildStoreByName(migration.MigrationStore),
		"the migration tree must not be auto-mounted outside migration modes")
}

// =============================================================================
// Export / Import Tests
// =============================================================================

// exportedItem stores one item produced by an exporter (module name or snapshot node).
type exportedItem struct {
	moduleName string
	node       *types.SnapshotNode
}

// drainCompositeExporter collects all items from an exporter in stream order.
func drainCompositeExporter(t *testing.T, exp types.Exporter) []exportedItem {
	t.Helper()
	var items []exportedItem
	for {
		raw, err := exp.Next()
		if err != nil {
			require.True(t, errors.Is(err, errorutils.ErrorExportDone), "unexpected error: %v", err)
			break
		}
		switch v := raw.(type) {
		case string:
			items = append(items, exportedItem{moduleName: v})
		case *types.SnapshotNode:
			items = append(items, exportedItem{node: v})
		default:
			t.Fatalf("unexpected item type %T", raw)
		}
	}
	return items
}

// replayImport feeds exported items into an importer.
func replayImport(t *testing.T, imp types.Importer, items []exportedItem) {
	t.Helper()
	for _, it := range items {
		if it.moduleName != "" {
			require.NoError(t, imp.AddModule(it.moduleName))
		} else {
			imp.AddNode(it.node)
		}
	}
}

// evmMigratedConfig returns a StateCommitConfig with EVMMigrated mode and
// fast snapshot intervals so that memiavl snapshots exist for the exporter.
func evmMigratedConfig() config.StateCommitConfig {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.EVMMigrated
	cfg.MemIAVLConfig.SnapshotInterval = 1
	cfg.MemIAVLConfig.SnapshotMinTimeInterval = 0
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0
	// With SnapshotInterval=1 every commit produces a snapshot, and FlatKV
	// mirrors this cadence via alignFlatKVSnapshotWithMemIAVL. The default
	// keep-recent of 1 would prune all but the two newest snapshots, so a
	// rollback/reconcile to an older version (e.g. v3 after committing v5)
	// could no longer find a base snapshot at-or-below the target. Retain all
	// snapshots for the short duration of a test so those paths stay valid.
	cfg.MemIAVLConfig.SnapshotKeepRecent = 100
	return cfg
}

func TestExportImportEVMMigrated(t *testing.T) {
	cfg := evmMigratedConfig()

	// --- Source store: write cosmos + EVM data ---
	srcDir := t.TempDir()
	src, err := NewCompositeCommitStore(t.Context(), srcDir, cfg)
	require.NoError(t, err)
	require.NoError(t, src.Initialize([]string{"bank", keys.EVMStoreKey}))
	_, err = src.LoadVersion(0, false)
	require.NoError(t, err)

	addr := ktype.Address{0xAA}
	slot := ktype.Slot{0xBB}
	storageKey := keys.BuildEVMKey(keys.EVMKeyStorage,
		ktype.StorageKey(addr, slot))
	storageVal := padLeft32(0x42)

	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	nonceVal := []byte{0, 0, 0, 0, 0, 0, 0, 10}

	err = src.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("balance_alice"), Value: []byte("100")},
		}}},
		{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: storageKey, Value: storageVal},
			{Key: nonceKey, Value: nonceVal},
		}}},
	})
	require.NoError(t, err)
	_, err = src.Commit()
	require.NoError(t, err)

	// --- Export ---
	exporter, err := src.Exporter(1)
	require.NoError(t, err)
	items := drainCompositeExporter(t, exporter)
	require.NoError(t, exporter.Close())
	require.NoError(t, src.Close())

	// Verify export stream structure: cosmos modules first, evm_flatkv last.
	var moduleNames []string
	for _, it := range items {
		if it.moduleName != "" {
			moduleNames = append(moduleNames, it.moduleName)
		}
	}
	require.Contains(t, moduleNames, "bank")
	require.Contains(t, moduleNames, keys.FlatKVStoreKey)
	// evm_flatkv should be the last module
	require.Equal(t, keys.FlatKVStoreKey, moduleNames[len(moduleNames)-1])

	// --- Destination store: import ---
	dstDir := t.TempDir()
	dst, err := NewCompositeCommitStore(t.Context(), dstDir, cfg)
	require.NoError(t, err)
	require.NoError(t, dst.Initialize([]string{"bank", keys.EVMStoreKey}))
	_, err = dst.LoadVersion(0, false)
	require.NoError(t, err)
	require.NoError(t, dst.Close())

	importer, err := dst.Importer(1)
	require.NoError(t, err)
	replayImport(t, importer, items)
	require.NoError(t, importer.Close())

	// Reload the store at version 1 to verify
	_, err = dst.LoadVersion(1, false)
	require.NoError(t, err)
	defer dst.Close()

	// Verify cosmos data
	bankStore := dst.GetChildStoreByName("bank")
	require.NotNil(t, bankStore)
	require.Equal(t, []byte("100"), bankStore.Get([]byte("balance_alice")))

	// Verify FlatKV data
	require.NotNil(t, dst.flatKV)
	got, found := dst.flatKV.Get(keys.EVMStoreKey, storageKey)
	require.True(t, found, "storage key should exist in FlatKV after import")
	require.Equal(t, storageVal, got)

	got, found = dst.flatKV.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, found, "nonce key should exist in FlatKV after import")
	require.Equal(t, nonceVal, got)
}

func TestExportMemiavlOnlyHasNoFlatKVModule(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.MemIAVLConfig.SnapshotInterval = 1
	cfg.MemIAVLConfig.SnapshotMinTimeInterval = 0
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0

	dir := t.TempDir()
	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{"bank"}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("key1"), Value: []byte("val1")},
		}}},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)

	exporter, err := cs.Exporter(1)
	require.NoError(t, err)
	items := drainCompositeExporter(t, exporter)
	require.NoError(t, exporter.Close())
	require.NoError(t, cs.Close())

	// In MemiavlOnly mode, evm_flatkv should NOT appear
	for _, it := range items {
		require.NotEqual(t, keys.FlatKVStoreKey, it.moduleName,
			"evm_flatkv should not appear in MemiavlOnly export")
	}
}

// TestExporterFailsLoudOnInHistoryFlatKVLoadFailure verifies that when
// flatkv fails to load at an export version within flatkv's history
// (version >= EarliestVersion), Exporter returns an error rather than
// silently emitting a memiavl-only snapshot that would drop
// consensus-visible flatkv state. Mirrors readOnlyTargetPredatesFlatKV's
// fail-loud contract for pruned/corrupt in-history versions.
func TestExporterFailsLoudOnInHistoryFlatKVLoadFailure(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0
	cfg.WriteMode = types.MigrateEVM
	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.SetMigrationBatchSize(100))
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("key1"), Value: []byte("value1")},
		}}},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)

	// Inject a flatkv whose load fails at an in-history version: export
	// version 1 is >= EarliestVersion 1, so the pre-era short-circuit does
	// not apply and the load failure must surface as an error.
	cs.flatKV = &eraFailingEVMStore{earliest: 1}

	_, err = cs.Exporter(1)
	require.Error(t, err, "Exporter must fail loud on an in-history flatkv load failure")
	require.Contains(t, err.Error(), "failed to load flatkv at export version")
}

// TestExporterOmitsFlatKVForPreEraVersion verifies that when the export
// version predates flatkv's history (version < EarliestVersion), Exporter
// omits flatkv and returns a memiavl-only snapshot without error — the
// flatkv load is never attempted. This is the legitimate pre-era case that
// must remain non-fatal even though a load at that version would fail.
func TestExporterOmitsFlatKVForPreEraVersion(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()
	cfg.MemIAVLConfig.SnapshotInterval = 1
	cfg.MemIAVLConfig.SnapshotMinTimeInterval = 0
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0
	cfg.WriteMode = types.MigrateEVM
	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.SetMigrationBatchSize(100))
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("key1"), Value: []byte("val1")},
		}}},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)

	// Inject a flatkv whose EarliestVersion is above the export height, so
	// version 1 is pre-era. LoadVersion would fail, but the pre-era check
	// short-circuits before it is called: flatkv is omitted, no error.
	cs.flatKV = &eraFailingEVMStore{earliest: 10}

	exporter, err := cs.Exporter(1)
	require.NoError(t, err, "pre-era export must omit flatkv without error")
	items := drainCompositeExporter(t, exporter)
	require.NoError(t, exporter.Close())
	require.NoError(t, cs.Close())

	for _, it := range items {
		require.NotEqual(t, keys.FlatKVStoreKey, it.moduleName,
			"flatkv module must not appear in a pre-era export")
	}
}

func TestCompositeImporterRouting(t *testing.T) {
	// Verify that the composite importer routes evm_flatkv exclusively
	// to the evm importer and other modules only to cosmos.
	var cosmosModules, evmModules []string
	var cosmosNodes, evmNodes []*types.SnapshotNode

	cosmosImp := &trackingImporter{
		modules: &cosmosModules,
		nodes:   &cosmosNodes,
	}
	evmImp := &trackingImporter{
		modules: &evmModules,
		nodes:   &evmNodes,
	}

	imp := NewImporter(cosmosImp, evmImp, nil)

	require.NoError(t, imp.AddModule("bank"))
	imp.AddNode(&types.SnapshotNode{Key: []byte("k1"), Value: []byte("v1")})

	require.NoError(t, imp.AddModule(keys.FlatKVStoreKey))
	imp.AddNode(&types.SnapshotNode{Key: []byte("k2"), Value: []byte("v2")})

	require.NoError(t, imp.AddModule("staking"))
	imp.AddNode(&types.SnapshotNode{Key: []byte("k3"), Value: []byte("v3")})

	// bank and staking → cosmos importer only
	require.Equal(t, []string{"bank", "staking"}, cosmosModules)
	require.Len(t, cosmosNodes, 2)
	require.Equal(t, []byte("k1"), cosmosNodes[0].Key)
	require.Equal(t, []byte("k3"), cosmosNodes[1].Key)

	// evm_flatkv → evm only
	require.Equal(t, []string{keys.FlatKVStoreKey}, evmModules)
	require.Len(t, evmNodes, 1)
	require.Equal(t, []byte("k2"), evmNodes[0].Key)

	require.NoError(t, imp.Close())
}

// trackingImporter records calls for test assertions.
type trackingImporter struct {
	modules *[]string
	nodes   *[]*types.SnapshotNode
}

func (ti *trackingImporter) AddModule(name string) error {
	*ti.modules = append(*ti.modules, name)
	return nil
}

func (ti *trackingImporter) AddNode(node *types.SnapshotNode) {
	*ti.nodes = append(*ti.nodes, node)
}

func (ti *trackingImporter) Close() error { return nil }

func TestReconcileVersionsAfterCrash(t *testing.T) {
	addr := [20]byte{0xAA}
	slot := [32]byte{0xBB}
	storageKey := keys.BuildEVMKey(keys.EVMKeyStorage,
		ktype.StorageKey(addr, slot))

	cfg := evmMigratedConfig()

	dir := t.TempDir()
	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	for i := byte(1); i <= 3; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: keys.BankStoreKey,
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
						{Key: []byte("key"), Value: []byte{i}},
					},
				},
			},
			{
				Name: keys.EVMStoreKey,
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
						{Key: storageKey, Value: padLeft32(i)},
					},
				},
			},
		})
		require.NoError(t, err)
		_, err = cs.Commit()
		require.NoError(t, err)
	}
	require.Equal(t, int64(3), cs.memIAVL.Version())
	require.Equal(t, int64(3), cs.flatKV.Version())
	require.NoError(t, cs.Close())

	// Simulate crash: rollback FlatKV to version 2 independently, leaving
	// cosmos at version 3. This mirrors a crash after cosmos Commit but
	// before FlatKV Commit completes.

	flatkvCfg := cfg.FlatKVConfig
	flatkvCfg.DataDir = utils.GetFlatKVPath(dir)
	evmStore, err := flatkv.NewCommitStore(t.Context(), &flatkvCfg)
	require.NoError(t, err)
	_, err = evmStore.LoadVersion(0, false)
	require.NoError(t, err)
	require.Equal(t, int64(3), evmStore.Version())
	err = evmStore.Rollback(2)
	require.NoError(t, err)
	require.Equal(t, int64(2), evmStore.Version())
	require.NoError(t, evmStore.Close())

	// Reopen the composite store — LoadVersion(0) should detect the
	// mismatch and reconcile both backends to version 2.
	cs2, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs2.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs2.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs2.Close()

	require.Equal(t, int64(2), cs2.memIAVL.Version(), "cosmos should be rolled back to EVM version")
	require.Equal(t, int64(2), cs2.flatKV.Version(), "EVM should remain at version 2")
	require.Equal(t, int64(2), cs2.Version())

	// Verify cosmos data is at version 2 (value = 0x02, not 0x03)
	testStore := cs2.GetChildStoreByName(keys.BankStoreKey)
	require.NotNil(t, testStore)
	require.Equal(t, []byte{2}, testStore.Get([]byte("key")))
}

func TestReconcileVersionsThenContinueCommitting(t *testing.T) {
	addr := [20]byte{0xEE}
	slot := [32]byte{0xFF}
	storageKey := keys.BuildEVMKey(keys.EVMKeyStorage,
		ktype.StorageKey(addr, slot))

	cfg := evmMigratedConfig()

	dir := t.TempDir()
	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{"bank", keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Commit versions 1-3 with both backends in sync.
	for i := byte(1); i <= 3; i++ {
		require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("bal"), Value: []byte{i}},
			}}},
			{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: storageKey, Value: padLeft32(i)},
			}}},
		}))
		_, err = cs.Commit()
		require.NoError(t, err)
	}
	require.NoError(t, cs.Close())

	// Simulate crash: roll FlatKV back to version 2.
	flatkvCfg := cfg.FlatKVConfig
	flatkvCfg.DataDir = utils.GetFlatKVPath(dir)
	evmStore, err := flatkv.NewCommitStore(t.Context(), &flatkvCfg)
	require.NoError(t, err)
	_, err = evmStore.LoadVersion(0, false)
	require.NoError(t, err)
	require.NoError(t, evmStore.Rollback(2))
	require.NoError(t, evmStore.Close())

	// Reopen — reconciliation should bring both to version 2.
	cs2, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs2.Initialize([]string{"bank", keys.EVMStoreKey}))
	_, err = cs2.LoadVersion(0, false)
	require.NoError(t, err)

	require.Equal(t, int64(2), cs2.memIAVL.Version())
	require.Equal(t, int64(2), cs2.flatKV.Version())

	// Continue committing new blocks on top of the reconciled state.
	// Version 3 is re-created with new data (0xA3 instead of 0x03).
	for i := byte(0); i < 3; i++ {
		v := 0xA0 + i + 3
		require.NoError(t, cs2.ApplyChangeSets([]*proto.NamedChangeSet{
			{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("bal"), Value: []byte{v}},
			}}},
			{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: storageKey, Value: padLeft32(v)},
			}}},
		}))
		ver, err := cs2.Commit()
		require.NoError(t, err)
		require.Equal(t, int64(3+i), ver, "commit should produce sequential versions")
		require.Equal(t, ver, cs2.memIAVL.Version())
		require.Equal(t, ver, cs2.flatKV.Version())
	}
	require.NoError(t, cs2.Close())

	// Reopen a third time to verify the post-reconciliation commits are durable
	// and both backends agree on version 5.
	cs3, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs3.Initialize([]string{"bank", keys.EVMStoreKey}))
	_, err = cs3.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs3.Close()

	require.Equal(t, int64(5), cs3.memIAVL.Version())
	require.Equal(t, int64(5), cs3.flatKV.Version())

	bankStore := cs3.GetChildStoreByName("bank")
	require.Equal(t, []byte{0xA5}, bankStore.Get([]byte("bal")))

	got, found := cs3.flatKV.Get(keys.EVMStoreKey, storageKey)
	require.True(t, found)
	require.Equal(t, padLeft32(0xA5), got)
}

// =============================================================================
// Per-store read methods: Get / Has / Iterator / GetProof
// =============================================================================

// setupComposite opens a fresh CompositeCommitStore using the given write
// mode, populates keys.BankStoreKey with k1->v1, k2->v2, k3->v3, commits version 1,
// and returns the store ready for read assertions. Cleanup is registered.
func setupComposite(t *testing.T, writeMode types.WriteMode) *CompositeCommitStore {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = writeMode

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.StakingStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k1"), Value: []byte("v1")},
			{Key: []byte("k2"), Value: []byte("v2")},
			{Key: []byte("k3"), Value: []byte("v3")},
		}}},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)
	return cs
}

func TestCompositeGetValidation(t *testing.T) {
	cs := setupComposite(t, types.MemiavlOnly)

	cases := []struct {
		name    string
		store   string
		key     []byte
		wantMsg string
	}{
		{"empty store", "", []byte("k1"), "store name cannot be empty"},
		{"nil key", keys.BankStoreKey, nil, "key cannot be nil"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := cs.Get(tc.store, tc.key)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}

// TestCompositeGetUnknownStore pins the current router-based contract:
// reading from a name the router cannot route returns an error. This
// behavior will relax to silent-miss once the router becomes a
// flatkv-style prefix passthrough; for now the router rejects.
func TestCompositeGetUnknownStore(t *testing.T) {
	cs := setupComposite(t, types.MemiavlOnly)
	_, _, err := cs.Get("nonexistent", []byte("k1"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "nonexistent")
}

func TestCompositeGetMissingKey(t *testing.T) {
	cs := setupComposite(t, types.MemiavlOnly)
	val, ok, err := cs.Get(keys.BankStoreKey, []byte("missing"))
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, val)
}

func TestCompositeGetPresent(t *testing.T) {
	cs := setupComposite(t, types.MemiavlOnly)
	val, ok, err := cs.Get(keys.BankStoreKey, []byte("k1"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("v1"), val)
}

func TestCompositeHasValidation(t *testing.T) {
	cs := setupComposite(t, types.MemiavlOnly)

	cases := []struct {
		name  string
		store string
		key   []byte
	}{
		{"empty store", "", []byte("k1")},
		{"nil key", keys.BankStoreKey, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cs.Has(tc.store, tc.key)
			require.Error(t, err)
		})
	}
}

// TestCompositeHasUnknownStore mirrors TestCompositeGetUnknownStore for Has.
func TestCompositeHasUnknownStore(t *testing.T) {
	cs := setupComposite(t, types.MemiavlOnly)
	_, err := cs.Has("nonexistent", []byte("k1"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "nonexistent")
}

func TestCompositeHasAgreesWithGet(t *testing.T) {
	cs := setupComposite(t, types.MemiavlOnly)
	testKeys := [][]byte{
		[]byte("k1"),
		[]byte("k2"),
		[]byte("k3"),
		[]byte("missing"),
	}
	for _, k := range testKeys {
		_, getOk, err := cs.Get(keys.BankStoreKey, k)
		require.NoError(t, err)
		hasOk, err := cs.Has(keys.BankStoreKey, k)
		require.NoError(t, err)
		require.Equal(t, getOk, hasOk, "Has should agree with Get for key %q", k)
	}
}

func TestCompositeIteratorValidation(t *testing.T) {
	cs := setupComposite(t, types.MemiavlOnly)

	cases := []struct {
		name  string
		store string
		start []byte
		end   []byte
	}{
		{"empty store", "", []byte("k1"), []byte("k9")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cs.Iterator(tc.store, tc.start, tc.end, true)
			require.Error(t, err)
		})
	}
}

// TestCompositeIteratorNilBounds pins the standard dbm.Iterator contract:
// a nil start/end means unbounded, so Iterator(nil, nil) is a full-store scan.
func TestCompositeIteratorNilBounds(t *testing.T) {
	cs := setupComposite(t, types.MemiavlOnly)
	iter, err := cs.Iterator(keys.BankStoreKey, nil, nil, true)
	require.NoError(t, err)
	require.NotNil(t, iter)
	defer iter.Close()

	var got []string
	for ; iter.Valid(); iter.Next() {
		got = append(got, string(iter.Key()))
	}
	require.NoError(t, iter.Error())
	require.Equal(t, []string{"k1", "k2", "k3"}, got)
}

// TestCompositeIteratorUnknownStore pins the no-op-on-unknown-store
// contract: a backend that does not hold the store contributes nothing,
// so iterating an unknown store yields a valid, empty iterator rather
// than an error. This matches the long-term flatkv-only end state where
// "unsupported store" ceases to exist.
func TestCompositeIteratorUnknownStore(t *testing.T) {
	cs := setupComposite(t, types.MemiavlOnly)
	iter, err := cs.Iterator("nonexistent", []byte("k1"), []byte("k9"), true)
	require.NoError(t, err)
	require.NotNil(t, iter)
	defer iter.Close()
	require.False(t, iter.Valid(), "unknown store must iterate as an empty range")
	require.NoError(t, iter.Error())
}

func TestCompositeIteratorAscending(t *testing.T) {
	cs := setupComposite(t, types.MemiavlOnly)
	iter, err := cs.Iterator(keys.BankStoreKey, []byte("k1"), []byte("k9"), true)
	require.NoError(t, err)
	require.NotNil(t, iter)
	defer iter.Close()

	var got []string
	for ; iter.Valid(); iter.Next() {
		got = append(got, string(iter.Key()))
	}
	require.NoError(t, iter.Error())
	require.Equal(t, []string{"k1", "k2", "k3"}, got)
}

func TestCompositeIteratorDescending(t *testing.T) {
	cs := setupComposite(t, types.MemiavlOnly)
	iter, err := cs.Iterator(keys.BankStoreKey, []byte("k1"), []byte("k9"), false)
	require.NoError(t, err)
	require.NotNil(t, iter)
	defer iter.Close()

	var got []string
	for ; iter.Valid(); iter.Next() {
		got = append(got, string(iter.Key()))
	}
	require.NoError(t, iter.Error())
	require.Equal(t, []string{"k3", "k2", "k1"}, got)
}

// TestCompositeIteratorRange pins the standard dbm.Iterator contract:
// start is inclusive, end is exclusive.
func TestCompositeIteratorRange(t *testing.T) {
	cs := setupComposite(t, types.MemiavlOnly)
	iter, err := cs.Iterator(keys.BankStoreKey, []byte("k1"), []byte("k3"), true)
	require.NoError(t, err)
	require.NotNil(t, iter)
	defer iter.Close()

	var got []string
	for ; iter.Valid(); iter.Next() {
		got = append(got, string(iter.Key()))
	}
	require.NoError(t, iter.Error())
	require.Equal(t, []string{"k1", "k2"}, got)
}

func TestCompositeGetProofValidation(t *testing.T) {
	cs := setupComposite(t, types.MemiavlOnly)

	cases := []struct {
		name  string
		store string
		key   []byte
	}{
		{"empty store", "", []byte("k1")},
		{"nil key", keys.BankStoreKey, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cs.GetProof(tc.store, tc.key)
			require.Error(t, err)
		})
	}
}

// TestCompositeGetProofUnknownStore mirrors TestCompositeGetUnknownStore for GetProof.
func TestCompositeGetProofUnknownStore(t *testing.T) {
	cs := setupComposite(t, types.MemiavlOnly)
	_, err := cs.GetProof("nonexistent", []byte("k1"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "nonexistent")
}

func TestCompositeGetProofPresent(t *testing.T) {
	cs := setupComposite(t, types.MemiavlOnly)
	proof, err := cs.GetProof(keys.BankStoreKey, []byte("k1"))
	require.NoError(t, err)
	require.NotNil(t, proof)
}

// TestCompositeEVMMigratedEVMReadsAreVisible pins the router-based read
// contract: in EVMMigrated mode the router sends evm/ reads to FlatKV,
// so composite.Get / Has surface the data written via ApplyChangeSets
// the same way a direct FlatKV lookup does.
func TestCompositeEVMMigratedEVMReadsAreVisible(t *testing.T) {
	dir := t.TempDir()
	cfg := evmMigratedConfig()

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })

	addr := [20]byte{0xAA}
	slot := [32]byte{0xBB}
	evmKey := keys.BuildEVMKey(keys.EVMKeyStorage, append(addr[:], slot[:]...))
	evmVal := padLeft32(0x42)

	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: evmKey, Value: evmVal},
		}}},
	}))
	_, err = cs.Commit()
	require.NoError(t, err)

	// FlatKV holds the authoritative copy.
	require.NotNil(t, cs.flatKV)
	got, found := cs.flatKV.Get(keys.EVMStoreKey, evmKey)
	require.True(t, found, "EVM data should be present in FlatKV")
	require.Equal(t, evmVal, got)

	// The composite's Get/Has route through the router and surface the
	// same FlatKV value.
	val, ok, err := cs.Get(keys.EVMStoreKey, evmKey)
	require.NoError(t, err)
	require.True(t, ok, "composite.Get must surface FlatKV data through the router")
	require.Equal(t, evmVal, val)

	hasOk, err := cs.Has(keys.EVMStoreKey, evmKey)
	require.NoError(t, err)
	require.True(t, hasOk)
}

// TestCompositeMemiavlOnlyPassesThrough sanity-checks that for cosmos-named
// stores in MemiavlOnly mode, the composite's read methods produce the same
// results as the underlying memiavl backend.
func TestCompositeMemiavlOnlyPassesThrough(t *testing.T) {
	cs := setupComposite(t, types.MemiavlOnly)

	val, ok, err := cs.Get(keys.BankStoreKey, []byte("k2"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("v2"), val)

	hasOk, err := cs.Has(keys.BankStoreKey, []byte("k2"))
	require.NoError(t, err)
	require.True(t, hasOk)

	// Iteration through the composite should yield the same keys as the
	// underlying cosmos child store.
	iter, err := cs.Iterator(keys.BankStoreKey, []byte("k1"), []byte("k9"), true)
	require.NoError(t, err)
	require.NotNil(t, iter)
	defer iter.Close()
	var got []string
	for ; iter.Valid(); iter.Next() {
		got = append(got, string(iter.Key()))
	}
	require.NoError(t, iter.Error())
	require.Equal(t, []string{"k1", "k2", "k3"}, got)
}

func TestReconcileVersionsCosmosAheadByMultiple(t *testing.T) {
	addr := [20]byte{0xCC}
	slot := [32]byte{0xDD}
	storageKey := keys.BuildEVMKey(keys.EVMKeyStorage,
		ktype.StorageKey(addr, slot))

	cfg := evmMigratedConfig()

	dir := t.TempDir()
	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{"bank", keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	for i := byte(1); i <= 5; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: "bank",
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
						{Key: []byte("bal"), Value: []byte{i}},
					},
				},
			},
			{
				Name: keys.EVMStoreKey,
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
						{Key: storageKey, Value: padLeft32(i)},
					},
				},
			},
		})
		require.NoError(t, err)
		_, err = cs.Commit()
		require.NoError(t, err)
	}
	require.NoError(t, cs.Close())

	// Rollback FlatKV to version 3 (simulating 2 lost commits)
	flatkvCfg := cfg.FlatKVConfig
	flatkvCfg.DataDir = utils.GetFlatKVPath(dir)
	evmStore, err := flatkv.NewCommitStore(t.Context(), &flatkvCfg)
	require.NoError(t, err)
	_, err = evmStore.LoadVersion(0, false)
	require.NoError(t, err)
	err = evmStore.Rollback(3)
	require.NoError(t, err)
	require.NoError(t, evmStore.Close())

	cs2, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs2.Initialize([]string{"bank", keys.EVMStoreKey}))
	_, err = cs2.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs2.Close()

	require.Equal(t, int64(3), cs2.memIAVL.Version())
	require.Equal(t, int64(3), cs2.flatKV.Version())

	bankStore := cs2.GetChildStoreByName("bank")
	require.Equal(t, []byte{3}, bankStore.Get([]byte("bal")))
}

// TestMigrationEntrySeedingMemiavlToMigrateEVM exercises the production
// scenario the seeding logic in composite.LoadVersion exists for: a chain
// that has been running on MemiavlOnly for many blocks switches its
// configuration to MigrateEVM at restart. memiavl is at version N (large),
// flatkv has never existed. The composite store must bring flatkv into
// lockstep at version N so subsequent commits produce matching versions
// on both backends. Without the SetInitialVersion seeding, the next
// Commit produces memiavl=N+1 and flatkv=1, wedging the chain on the
// version-mismatch guard.
func TestMigrationEntrySeedingMemiavlToMigrateEVM(t *testing.T) {
	dir := t.TempDir()

	// Phase 1: run for 100 blocks in MemiavlOnly mode.
	cosmosCfg := config.DefaultStateCommitConfig()
	cosmosCfg.WriteMode = types.MemiavlOnly

	cs1, err := NewCompositeCommitStore(t.Context(), dir, cosmosCfg)
	require.NoError(t, err)
	require.NoError(t, cs1.Initialize([]string{"bank", keys.EVMStoreKey}))
	_, err = cs1.LoadVersion(0, false)
	require.NoError(t, err)

	const phase1Blocks = 100
	for i := 0; i < phase1Blocks; i++ {
		err := cs1.ApplyChangeSets([]*proto.NamedChangeSet{
			{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte(fmt.Sprintf("bal_%d", i)), Value: []byte{byte(i)}},
			}}},
		})
		require.NoError(t, err)
		v, err := cs1.Commit()
		require.NoError(t, err)
		require.Equal(t, int64(i+1), v)
	}
	require.Equal(t, int64(phase1Blocks), cs1.Version())
	require.Nil(t, cs1.flatKV, "MemiavlOnly mode must not create a flatkv store")
	require.NoError(t, cs1.Close())

	// Phase 2: reopen with MigrateEVM mode. memiavl is at version 100,
	// flatkv directory does not exist yet. Seeding must bring flatkv to
	// version 100 so the very next commit produces version 101 on both.
	migrateCfg := config.DefaultStateCommitConfig()
	migrateCfg.WriteMode = types.MigrateEVM
	cs2, err := NewCompositeCommitStore(t.Context(), dir, migrateCfg)
	require.NoError(t, err)
	require.NoError(t, cs2.SetMigrationBatchSize(100))
	require.NoError(t, cs2.Initialize([]string{"bank", keys.EVMStoreKey}))
	_, err = cs2.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs2.Close()

	require.Equal(t, int64(phase1Blocks), cs2.memIAVL.Version(),
		"memiavl version must survive reopen")
	require.NotNil(t, cs2.flatKV, "MigrateEVM mode must create a flatkv store")
	require.Equal(t, int64(phase1Blocks), cs2.flatKV.Version(),
		"flatkv must be seeded to memiavl's version after migration-entry seeding")
	require.Equal(t, int64(phase1Blocks), cs2.Version(),
		"composite version must report the seeded version")

	// Phase 3: drive more blocks through the migration router and verify
	// both backends advance in lockstep.
	const phase3Blocks = 10
	for i := 0; i < phase3Blocks; i++ {
		blockIdx := phase1Blocks + i
		err := cs2.ApplyChangeSets([]*proto.NamedChangeSet{
			{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte(fmt.Sprintf("bal_%d", blockIdx)), Value: []byte{byte(blockIdx)}},
			}}},
		})
		require.NoError(t, err)
		v, err := cs2.Commit()
		require.NoError(t, err)
		require.Equal(t, int64(blockIdx+1), v)
		require.Equal(t, cs2.memIAVL.Version(), cs2.flatKV.Version(),
			"memiavl and flatkv must stay in lockstep after seeding")
	}
}

func TestMigrateEVMReopenPreservesPreFlipLastCommitInfo(t *testing.T) {
	dir := t.TempDir()

	memCfg := config.DefaultStateCommitConfig()
	memCfg.WriteMode = types.MemiavlOnly
	memCfg.MemIAVLConfig.AsyncCommitBuffer = 0

	cs1, err := NewCompositeCommitStore(t.Context(), dir, memCfg)
	require.NoError(t, err)
	require.NoError(t, cs1.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs1.LoadVersion(0, false)
	require.NoError(t, err)

	addr := [20]byte{0xA1}
	slot := [32]byte{0xB2}
	evmKey := keys.BuildEVMKey(keys.EVMKeyStorage, append(addr[:], slot[:]...))
	for i := byte(1); i <= 3; i++ {
		require.NoError(t, cs1.ApplyChangeSets([]*proto.NamedChangeSet{
			{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("bal"), Value: []byte{i}},
			}}},
			{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: evmKey, Value: padLeft32(i)},
			}}},
		}))
		_, err = cs1.Commit()
		require.NoError(t, err)
	}
	require.Nil(t, cs1.flatKV, "MemiavlOnly must not allocate flatkv before the migration")
	require.NoError(t, cs1.Close())

	preFlipVersion := int64(3)

	migrateCfg := config.DefaultStateCommitConfig()
	migrateCfg.WriteMode = types.MigrateEVM
	migrateCfg.MemIAVLConfig.AsyncCommitBuffer = 0

	cs2, err := NewCompositeCommitStore(t.Context(), dir, migrateCfg)
	require.NoError(t, err)
	require.NoError(t, cs2.SetMigrationBatchSize(1))
	require.NoError(t, cs2.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs2.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() { _ = cs2.Close() }()

	require.Equal(t, preFlipVersion, cs2.Version())
	lastAtMigration := cs2.LastCommitInfo()
	for _, si := range lastAtMigration.StoreInfos {
		require.NotEqual(t, "evm_lattice", si.Name,
			"opening migrate_evm must be AppHash-neutral at the already-committed height")
	}
	hasLattice := func(info *proto.CommitInfo) bool {
		for _, si := range info.StoreInfos {
			if si.Name == "evm_lattice" {
				return true
			}
		}
		return false
	}

	require.NoError(t, cs2.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("bal"), Value: []byte{0xFF}},
		}}},
	}))
	working := cs2.WorkingCommitInfo()
	require.True(t, hasLattice(working),
		"the next block after the migration should include the flatkv lattice hash")

	_, err = cs2.Commit()
	require.NoError(t, err)
	last := cs2.LastCommitInfo()
	require.True(t, hasLattice(last))
}

// TestMigrationEntrySeedingIsIdempotentAcrossRestarts verifies that once
// flatkv has been seeded and committed, a subsequent restart does not
// re-seed (which would error out via the "non-empty store" guard).
func TestMigrationEntrySeedingIsIdempotentAcrossRestarts(t *testing.T) {
	dir := t.TempDir()

	cosmosCfg := config.DefaultStateCommitConfig()
	cosmosCfg.WriteMode = types.MemiavlOnly
	cs1, err := NewCompositeCommitStore(t.Context(), dir, cosmosCfg)
	require.NoError(t, err)
	require.NoError(t, cs1.Initialize([]string{"bank", keys.EVMStoreKey}))
	_, err = cs1.LoadVersion(0, false)
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		require.NoError(t, cs1.ApplyChangeSets([]*proto.NamedChangeSet{
			{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("bal"), Value: []byte{byte(i)}},
			}}},
		}))
		_, err := cs1.Commit()
		require.NoError(t, err)
	}
	require.NoError(t, cs1.Close())

	migrateCfg := config.DefaultStateCommitConfig()
	migrateCfg.WriteMode = types.MigrateEVM
	cs2, err := NewCompositeCommitStore(t.Context(), dir, migrateCfg)
	require.NoError(t, err)
	require.NoError(t, cs2.SetMigrationBatchSize(100))
	require.NoError(t, cs2.Initialize([]string{"bank", keys.EVMStoreKey}))
	_, err = cs2.LoadVersion(0, false)
	require.NoError(t, err)
	require.Equal(t, int64(5), cs2.flatKV.Version(), "flatkv seeded to memiavl version on first reopen")
	_, err = cs2.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(6), cs2.Version())
	require.NoError(t, cs2.Close())

	cs3, err := NewCompositeCommitStore(t.Context(), dir, migrateCfg)
	require.NoError(t, err)
	require.NoError(t, cs3.Initialize([]string{"bank", keys.EVMStoreKey}))
	_, err = cs3.LoadVersion(0, false)
	require.NoError(t, err, "second reopen must not re-seed flatkv (would fail the fresh-store guard)")
	defer cs3.Close()
	require.Equal(t, int64(6), cs3.memIAVL.Version())
	require.Equal(t, int64(6), cs3.flatKV.Version())
}

// TestInitializeIsNoOpInFlatKVOnly verifies that composite.Initialize does
// not dereference a nil memIAVL when running in FlatKVOnly mode. flatkv has
// no per-module pre-allocation analog, so the call is a no-op there.
func TestInitializeIsNoOpInFlatKVOnly(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.FlatKVOnly

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	require.Nil(t, cs.memIAVL, "FlatKVOnly must not allocate a memIAVL backend")
	require.NotPanics(t, func() {
		require.NoError(t, cs.Initialize([]string{"bank", keys.EVMStoreKey}))
	}, "Initialize must not panic when memIAVL is nil")
}

// TestSetInitialVersionMemiavlOnly verifies SetInitialVersion delegates
// only to memIAVL when flatkv is absent, and that the first commit
// produces the requested version.
func TestSetInitialVersionMemiavlOnly(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MemiavlOnly

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{"bank", keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs.Close()
	require.Nil(t, cs.flatKV, "MemiavlOnly must not allocate a flatkv backend")

	require.NoError(t, cs.SetInitialVersion(100))

	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("alice"), Value: []byte("1")},
		}}},
	}))
	v, err := cs.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(100), v, "first commit after SetInitialVersion(100) must be version 100")
}

// TestSetInitialVersionDelegatesToBothBackends verifies that in a mode
// where both backends are active (MigrateEVM), SetInitialVersion seeds
// both and the next commit produces matching versions.
func TestSetInitialVersionDelegatesToBothBackends(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MigrateEVM
	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	require.NoError(t, cs.SetMigrationBatchSize(100))
	require.NoError(t, cs.Initialize([]string{"bank", keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs.Close()
	require.NotNil(t, cs.memIAVL)
	require.NotNil(t, cs.flatKV)

	require.NoError(t, cs.SetInitialVersion(50))

	require.Equal(t, int64(49), cs.flatKV.Version(),
		"flatkv reflects the seed immediately (committedVersion = N-1)")

	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("alice"), Value: []byte("1")},
		}}},
	}))
	v, err := cs.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(50), v,
		"first commit after composite.SetInitialVersion must produce the seeded version on both backends")
	require.Equal(t, int64(50), cs.memIAVL.Version())
	require.Equal(t, int64(50), cs.flatKV.Version())
}

// TestSetInitialVersionRetryIsIdempotent verifies that a caller retrying
// SetInitialVersion with the same value (e.g. after a transient failure)
// does not wedge the store. Memiavl-first ordering matters here: memiavl
// permits a second call while no commit has happened, and flatkv would
// have rejected the second call had it already succeeded once.
func TestSetInitialVersionRetryIsIdempotent(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MigrateEVM
	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	require.NoError(t, cs.SetMigrationBatchSize(100))
	require.NoError(t, cs.Initialize([]string{"bank", keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs.Close()

	// First call seeds both backends.
	require.NoError(t, cs.SetInitialVersion(75))
	// Second call: memiavl is still pre-commit, so its idempotency holds;
	// but flatkv is already at committedVersion=74 and rejects the retry.
	err = cs.SetInitialVersion(75)
	require.Error(t, err, "the second call must surface flatkv's fresh-store rejection")
	require.Contains(t, err.Error(), "flatkv SetInitialVersion")
}

// TestInitializeRejectsUnknownStoreNames verifies that
// composite.Initialize fails fast when given names the router cannot
// route. The ModuleRouter used in migration and steady-state modes
// only routes the canonical set in keys.MemIAVLStoreKeys; any other
// name (e.g. legacy test placeholders) is rejected before backend
// state is touched.
func TestInitializeRejectsUnknownStoreNames(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MigrateEVM

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()

	err = cs.Initialize([]string{keys.BankStoreKey, "bogus", "also-bogus"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not routable")
	require.Contains(t, err.Error(), "bogus")
	require.Contains(t, err.Error(), "also-bogus")
	require.NotContains(t, err.Error(), keys.BankStoreKey,
		"the valid name should not appear in the unknown-names list")
}

// TestInitializeAcceptsUnknownStoreNamesInMemiavlOnly is the
// regression test for the sei-ibc-go simapp failure: downstream test
// apps that mount more modules than seid (icahost / icacontroller)
// must be able to run in MemiavlOnly. The PassthroughRouter installed
// for that mode performs no name lookup, so Initialize must accept
// arbitrary names. The test follows up by writing through one of
// those non-canonical stores and reading the value back to confirm
// the full ApplyChangeSets / Commit / Get path actually works against
// memiavl for names outside keys.MemIAVLStoreKeys.
func TestInitializeAcceptsUnknownStoreNamesInMemiavlOnly(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MemiavlOnly

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()

	require.NoError(t, cs.Initialize([]string{"icahost", "icacontroller"}))

	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "icahost", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k"), Value: []byte("v")},
		}}},
	}))
	_, err = cs.Commit()
	require.NoError(t, err)

	got, ok, err := cs.Get("icahost", []byte("k"))
	require.NoError(t, err)
	require.True(t, ok, "PassthroughRouter must forward reads to memiavl for non-canonical names")
	require.Equal(t, []byte("v"), got)
}

// TestInitializeAcceptsUnknownStoreNamesInFlatKVOnly is the FlatKVOnly
// counterpart to TestInitializeAcceptsUnknownStoreNamesInMemiavlOnly.
// FlatKVOnly likewise uses a PassthroughRouter, so Initialize must
// accept arbitrary names and the full ApplyChangeSets / Commit / Get
// round-trip must work for them against the flatkv backend.
// memIAVL is intentionally nil in this mode; the test guards that
// Initialize stays a no-op for the memiavl side while still
// validating the name list.
func TestInitializeAcceptsUnknownStoreNamesInFlatKVOnly(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.FlatKVOnly

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	require.Nil(t, cs.memIAVL, "FlatKVOnly must not allocate a memIAVL backend")
	defer func() { _ = cs.Close() }()

	require.NoError(t, cs.Initialize([]string{"icahost", "icacontroller"}))

	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "icahost", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k"), Value: []byte("v")},
		}}},
	}))
	_, err = cs.Commit()
	require.NoError(t, err)

	got, ok, err := cs.Get("icahost", []byte("k"))
	require.NoError(t, err)
	require.True(t, ok, "PassthroughRouter must forward reads to flatkv for non-canonical names")
	require.Equal(t, []byte("v"), got)
}

// TestInitializeAcceptsAllMemIAVLStoreKeys verifies that the entire
// canonical production set passes validation. Guards against
// validateInitialStores drifting away from keys.MemIAVLStoreKeys.
func TestInitializeAcceptsAllMemIAVLStoreKeys(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MemiavlOnly

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()

	require.NoError(t, cs.Initialize(keys.MemIAVLStoreKeys))
}

// TestCopyProducesUsableSnapshot exercises the full snapshot path
// callers actually take: capture an SC snapshot via Copy, then read
// committed state through GetChildStoreByName. Regression for a bug
// where Copy returned a CompositeCommitStore with a nil router, so
// the first read through RouterCommitKVStore nil-derefed (the trace
// RPC path hit this via baseapp.GetConsensusParams). A second Copy
// of the snapshot must also be usable, since TraceSnapshotStore.Lease
// performs another Copy on top of the stored snapshot.
func TestCopyProducesUsableSnapshot(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MemiavlOnly

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()

	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k"), Value: []byte("v")},
		}}},
	}))
	_, err = cs.Commit()
	require.NoError(t, err)

	snap := cs.Copy()
	require.NotNil(t, snap, "Copy must return a non-nil snapshot in MemiavlOnly mode")
	defer func() {
		releaser, ok := snap.(interface{ ReleaseSnapshotRefs() error })
		require.True(t, ok)
		require.NoError(t, releaser.ReleaseSnapshotRefs())
	}()

	snapComposite, ok := snap.(*CompositeCommitStore)
	require.True(t, ok)
	bankSnap := snapComposite.GetChildStoreByName(keys.BankStoreKey)
	require.NotNil(t, bankSnap)
	require.NotPanics(t, func() {
		require.Equal(t, []byte("v"), bankSnap.Get([]byte("k")))
		require.True(t, bankSnap.Has([]byte("k")))
	}, "snapshot reads must not nil-deref on the snapshot's router")

	leased := snapComposite.Copy()
	require.NotNil(t, leased, "Copy of a snapshot must also produce a usable snapshot (Lease path)")
	defer func() {
		releaser, ok := leased.(interface{ ReleaseSnapshotRefs() error })
		require.True(t, ok)
		require.NoError(t, releaser.ReleaseSnapshotRefs())
	}()
	leasedComposite, ok := leased.(*CompositeCommitStore)
	require.True(t, ok)
	bankLeased := leasedComposite.GetChildStoreByName(keys.BankStoreKey)
	require.NotNil(t, bankLeased)
	require.NotPanics(t, func() {
		require.Equal(t, []byte("v"), bankLeased.Get([]byte("k")))
	}, "leased snapshot reads must not nil-deref")
}

// TestInitializeRejectsMigrationStoreName verifies that callers cannot
// inject the MigrationStore tree themselves. The composite mounts it
// on demand in LoadVersion when the mode requires it; accepting the
// name from outside would let callers smuggle a migration tree into
// state and confuse later upgrades. The reservation holds in every
// mode -- both MemiavlOnly (which otherwise has no allow-list) and
// migration modes -- so a misconfigured caller can't sneak it past
// the relaxed validation.
func TestInitializeRejectsMigrationStoreName(t *testing.T) {
	cases := []struct {
		name string
		mode types.WriteMode
	}{
		{"MemiavlOnly", types.MemiavlOnly},
		{"FlatKVOnly", types.FlatKVOnly},
		{"MigrateEVM", types.MigrateEVM},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultStateCommitConfig()
			cfg.WriteMode = tc.mode

			cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
			require.NoError(t, err)
			defer func() { _ = cs.Close() }()

			err = cs.Initialize([]string{migration.MigrationStore})
			require.Error(t, err)
			require.Contains(t, err.Error(), migration.MigrationStore)
		})
	}
}

// TestGetChildStoreByName_NameValidation exercises the per-mode
// validation in GetChildStoreByName across the full WriteMode matrix.
// The matrix covers all three classes of mode:
//
//   - MemiavlOnly: the only requirement is that the named tree exists
//     on the memiavl backend, so non-standard names are allowed if
//     they were registered via Initialize. A canonical key registered
//     via Initialize must work; an unregistered name (canonical or
//     otherwise) must panic.
//   - FlatKVOnly: arbitrary names are accepted unconditionally.
//   - All other modes (migration transitions, steady states, and
//     TestOnlyDualWrite): only names in keys.MemIAVLStoreKeys are
//     accepted. Any other name -- including the reserved
//     migration.MigrationStore tree -- must panic.
func TestGetChildStoreByName_NameValidation(t *testing.T) {
	const nonCanonical = "not-a-real-store"

	cases := []struct {
		modeName      string
		mode          types.WriteMode
		initialStores []string
		queryName     string
		wantPanic     bool
	}{
		{
			modeName:      "MemiavlOnly/canonical-registered",
			mode:          types.MemiavlOnly,
			initialStores: []string{keys.BankStoreKey},
			queryName:     keys.BankStoreKey,
		},
		{
			modeName:      "MemiavlOnly/non-canonical-registered",
			mode:          types.MemiavlOnly,
			initialStores: []string{nonCanonical},
			queryName:     nonCanonical,
		},
		{
			modeName:      "MemiavlOnly/canonical-unregistered",
			mode:          types.MemiavlOnly,
			initialStores: []string{keys.BankStoreKey},
			queryName:     keys.EVMStoreKey,
			wantPanic:     true,
		},
		{
			modeName:      "MemiavlOnly/non-canonical-unregistered",
			mode:          types.MemiavlOnly,
			initialStores: []string{keys.BankStoreKey},
			queryName:     nonCanonical,
			wantPanic:     true,
		},
		{
			modeName:      "MemiavlOnly/migration-store-is-reserved",
			mode:          types.MemiavlOnly,
			initialStores: []string{keys.BankStoreKey},
			queryName:     migration.MigrationStore,
			wantPanic:     true,
		},
		{
			modeName:  "FlatKVOnly/canonical",
			mode:      types.FlatKVOnly,
			queryName: keys.EVMStoreKey,
		},
		{
			modeName:  "FlatKVOnly/non-canonical",
			mode:      types.FlatKVOnly,
			queryName: nonCanonical,
		},
		{
			modeName:  "FlatKVOnly/migration-store-is-reserved",
			mode:      types.FlatKVOnly,
			queryName: migration.MigrationStore,
			wantPanic: true,
		},
		{
			modeName:      "MigrateEVM/canonical",
			mode:          types.MigrateEVM,
			initialStores: []string{keys.BankStoreKey, keys.EVMStoreKey},
			queryName:     keys.BankStoreKey,
		},
		{
			modeName:      "MigrateEVM/non-canonical",
			mode:          types.MigrateEVM,
			initialStores: []string{keys.BankStoreKey, keys.EVMStoreKey},
			queryName:     nonCanonical,
			wantPanic:     true,
		},
		{
			modeName:      "MigrateEVM/migration-store-is-reserved",
			mode:          types.MigrateEVM,
			initialStores: []string{keys.BankStoreKey, keys.EVMStoreKey},
			queryName:     migration.MigrationStore,
			wantPanic:     true,
		},
		{
			modeName:      "EVMMigrated/canonical",
			mode:          types.EVMMigrated,
			initialStores: []string{keys.BankStoreKey, keys.EVMStoreKey},
			queryName:     keys.BankStoreKey,
		},
		{
			modeName:      "EVMMigrated/non-canonical",
			mode:          types.EVMMigrated,
			initialStores: []string{keys.BankStoreKey, keys.EVMStoreKey},
			queryName:     nonCanonical,
			wantPanic:     true,
		},
		{
			modeName:      "TestOnlyDualWrite/non-canonical",
			mode:          types.TestOnlyDualWrite,
			initialStores: []string{keys.BankStoreKey, keys.EVMStoreKey},
			queryName:     nonCanonical,
			wantPanic:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.modeName, func(t *testing.T) {
			cfg := config.DefaultStateCommitConfig()
			cfg.WriteMode = tc.mode

			cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
			require.NoError(t, err)
			defer func() { _ = cs.Close() }()

			if len(tc.initialStores) > 0 {
				require.NoError(t, cs.Initialize(tc.initialStores))
			}
			_, err = cs.LoadVersion(0, false)
			require.NoError(t, err)

			if tc.wantPanic {
				require.Panics(t, func() {
					cs.GetChildStoreByName(tc.queryName)
				})
				return
			}

			require.NotPanics(t, func() {
				got := cs.GetChildStoreByName(tc.queryName)
				require.NotNil(t, got)
			})
		})
	}
}

// TestLoadVersionReadOnlyDuringMigrateEVMTransition reproduces the race
// flagged by the reviewer: a node that has been running in MemiavlOnly
// mode is restarted into MigrateEVM mode. The writable LoadVersion
// returns before the first migration block is committed. While the
// writable handle is live, a read-only LoadVersion (e.g. an ABCI
// historical query at version 0) must succeed and return correct
// pre-migration data.
//
// Before the fix, the migration manager probed memiavl for a "migration"
// tree that did not exist on disk, and the read-only handle failed with
// "store not found: migration". After deleting that tree and the dead
// probe, both writable and read-only paths bootstrap identically against
// a memiavl that never owns a migration tree.
//
// The test also pins the consensus-visible side: neither handle may
// surface a "migration" store in its CommitInfo, because that would
// silently expand the StoreInfos set hashed into the app hash.
func TestLoadVersionReadOnlyDuringMigrateEVMTransition(t *testing.T) {
	dir := t.TempDir()

	// Phase 1: run in MemiavlOnly mode, write some evm/ data, commit,
	// close. This produces an on-disk memiavl snapshot/WAL with no
	// "migration" tree.
	v0Cfg := config.DefaultStateCommitConfig()
	v0Cfg.WriteMode = types.MemiavlOnly

	cs1, err := NewCompositeCommitStore(t.Context(), dir, v0Cfg)
	require.NoError(t, err)
	require.NoError(t, cs1.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs1.LoadVersion(0, false)
	require.NoError(t, err)

	const evmKey = "evm_pre_migration"
	const evmVal = "v0-value"
	require.NoError(t, cs1.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte(evmKey), Value: []byte(evmVal)},
		}}},
	}))
	_, err = cs1.Commit()
	require.NoError(t, err)
	require.NoError(t, cs1.Close())

	// Phase 2: reopen in MigrateEVM mode. LoadVersion(0, false)
	// completes (seeding flatkv, building the router) but no migration
	// block has yet been committed. This is the window the reviewer
	// flagged.
	migrateCfg := config.DefaultStateCommitConfig()
	migrateCfg.WriteMode = types.MigrateEVM
	cs2, err := NewCompositeCommitStore(t.Context(), dir, migrateCfg)
	require.NoError(t, err)
	require.NoError(t, cs2.SetMigrationBatchSize(100))
	require.NoError(t, cs2.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs2.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs2.Close()

	// Memiavl must not own a "migration" tree on the writable handle.
	// If it did, the tree would contribute a StoreInfo entry to every
	// CommitInfo and so to the app hash.
	require.Nil(t, cs2.memIAVL.GetChildStoreByName(migration.MigrationStore),
		"writable handle must not materialize a migration tree on memiavl")
	for _, si := range cs2.WorkingCommitInfo().StoreInfos {
		require.NotEqual(t, migration.MigrationStore, si.Name,
			"writable handle's WorkingCommitInfo must not include a migration StoreInfo")
	}

	// While the writable handle is live and pre-first-commit, a
	// concurrent read-only LoadVersion must succeed.
	ro, err := cs2.LoadVersion(0, true)
	require.NoError(t, err,
		"read-only LoadVersion in MigrateEVM mode must succeed during the pre-first-commit window")
	defer func() { _ = ro.Close() }()

	roComposite, ok := ro.(*CompositeCommitStore)
	require.True(t, ok)
	require.NotSame(t, cs2, roComposite, "read-only LoadVersion returns an isolated handle")
	require.NotNil(t, roComposite.router, "read-only handle must have its own router")

	// The read-only memiavl also must not have a migration tree.
	require.Nil(t, roComposite.memIAVL.GetChildStoreByName(migration.MigrationStore),
		"read-only handle must not materialize a migration tree on memiavl")

	// The router on the read-only handle came up with boundary=NotStarted
	// (flatkv is empty, no MigrationVersionKey, default to startVersion=0
	// = Version0_MemiavlOnly). All evm/ reads route to memiavl, which
	// still has the pre-migration value.
	got, found, err := roComposite.Get(keys.EVMStoreKey, []byte(evmKey))
	require.NoError(t, err, "evm/ read must not fail with store-not-found on the read-only handle")
	require.True(t, found, "pre-migration evm/ value must be visible to the read-only handle")
	require.Equal(t, []byte(evmVal), got)
}

func TestAlignFlatKVSnapshotWithMemIAVL(t *testing.T) {
	t.Run("FlatKV derives interval and keep-recent from a non-zero memIAVL", func(t *testing.T) {
		cfg := config.DefaultStateCommitConfig()
		cfg.MemIAVLConfig.SnapshotInterval = 5000
		cfg.MemIAVLConfig.SnapshotKeepRecent = 3
		// Start FlatKV from divergent values to prove they get overwritten.
		cfg.FlatKVConfig.SnapshotInterval = 111
		cfg.FlatKVConfig.SnapshotKeepRecent = 222

		alignFlatKVSnapshotWithMemIAVL(&cfg)

		require.Equal(t, uint32(5000), cfg.FlatKVConfig.SnapshotInterval)
		require.Equal(t, uint32(3), cfg.FlatKVConfig.SnapshotKeepRecent)
	})

	t.Run("a zero memIAVL keep-recent resolves to the healed default", func(t *testing.T) {
		cfg := config.DefaultStateCommitConfig()
		cfg.MemIAVLConfig.SnapshotKeepRecent = 0
		// FlatKV must not mirror the raw 0 (which would prune everything but the
		// latest). Instead it mirrors the value FillDefaults will heal memIAVL to,
		// keeping the two in lockstep. memIAVL's own 0 is left for FillDefaults.
		alignFlatKVSnapshotWithMemIAVL(&cfg)

		require.Equal(t, uint32(0), cfg.MemIAVLConfig.SnapshotKeepRecent)
		require.Equal(t, uint32(memiavl.DefaultSnapshotKeepRecent), cfg.FlatKVConfig.SnapshotKeepRecent)
	})

	t.Run("a zero memIAVL interval resolves to the healed default", func(t *testing.T) {
		cfg := config.DefaultStateCommitConfig()
		cfg.MemIAVLConfig.SnapshotInterval = 0
		// A raw 0 would disable FlatKV auto-snapshots; instead FlatKV mirrors the
		// value FillDefaults will heal memIAVL's interval to.
		alignFlatKVSnapshotWithMemIAVL(&cfg)

		require.Equal(t, uint32(memiavl.DefaultSnapshotInterval), cfg.FlatKVConfig.SnapshotInterval)
		require.NotZero(t, cfg.FlatKVConfig.SnapshotInterval)
	})

	t.Run("an explicit FlatKV override loses to memIAVL's healed default", func(t *testing.T) {
		// Upgrade scenario: an old app.toml still pins an explicit FlatKV
		// keep-recent/interval (the previous template rendered flatkv.* keys)
		// while sc-* is 0. FlatKV must follow memIAVL's effective (healed) cadence
		// rather than staying pinned to the stale explicit value, otherwise the
		// two backends diverge (memIAVL heals 0 -> default, FlatKV keeps the old
		// explicit value).
		cfg := config.DefaultStateCommitConfig()
		cfg.MemIAVLConfig.SnapshotKeepRecent = 0
		cfg.MemIAVLConfig.SnapshotInterval = 0
		cfg.FlatKVConfig.SnapshotKeepRecent = 2
		cfg.FlatKVConfig.SnapshotInterval = 7777

		alignFlatKVSnapshotWithMemIAVL(&cfg)

		require.Equal(t, uint32(memiavl.DefaultSnapshotKeepRecent), cfg.FlatKVConfig.SnapshotKeepRecent)
		require.Equal(t, uint32(memiavl.DefaultSnapshotInterval), cfg.FlatKVConfig.SnapshotInterval)
	})
}
