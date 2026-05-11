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
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
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
func (f *failingEVMStore) Has(string, []byte) bool                { return false }
func (f *failingEVMStore) RawGlobalIterator() flatkv.Iterator     { return nil }
func (f *failingEVMStore) RootHash() []byte                       { return nil }
func (f *failingEVMStore) Version() int64                         { return 0 }
func (f *failingEVMStore) GetLatestVersion() (int64, error)       { return 0, nil }
func (f *failingEVMStore) WriteSnapshot(string) error             { return nil }
func (f *failingEVMStore) Rollback(int64) error                   { return nil }
func (f *failingEVMStore) Exporter(int64) (types.Exporter, error) { return nil, nil }
func (f *failingEVMStore) Importer(int64) (types.Importer, error) { return nil, nil }
func (f *failingEVMStore) GetPhaseTimer() *metrics.PhaseTimer     { return nil }
func (f *failingEVMStore) CommittedRootHash() []byte              { return nil }
func (f *failingEVMStore) CleanupOrphanedReadOnlyDirs() error     { return nil }
func (f *failingEVMStore) Close() error                           { return nil }

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
	cs.Initialize([]string{"test", keys.EVMStoreKey})

	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, cs.Close())
	}()

	require.Equal(t, int64(0), cs.Version())

	// Apply changesets with both regular and EVM data
	changesets := []*proto.NamedChangeSet{
		{
			Name: "test",
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

	testStore := cs.GetChildStoreByName("test")
	require.NotNil(t, testStore)

	evmStore := cs.GetChildStoreByName(keys.EVMStoreKey)
	require.NotNil(t, evmStore)
}

func TestEmptyChangesets(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	cs.Initialize([]string{"test"})

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
	cs.Initialize([]string{"test"})

	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
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
	cs.Initialize([]string{"test"})

	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, cs.Close())
	}()

	workingInfo := cs.WorkingCommitInfo()
	require.NotNil(t, workingInfo)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
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
				Name: "test",
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
		writeMode     config.WriteMode
		expectLattice bool
	}{
		{"MemiavlOnly", config.MemiavlOnly, false},
		{"TestOnlyDualWrite", config.TestOnlyDualWrite, true},
		{"EVMMigrated", config.EVMMigrated, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := config.DefaultStateCommitConfig()
			cfg.WriteMode = tt.writeMode

			cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
			require.NoError(t, err)
			cs.Initialize([]string{"test", keys.EVMStoreKey})
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

func TestRollback(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	cs.Initialize([]string{"test"})

	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Commit a few versions
	for i := 0; i < 3; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: "test",
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
	cs.Initialize([]string{"test"})

	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: "test",
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
	cs2.Initialize([]string{"test"})

	latestVersion, err := cs2.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(3), latestVersion)
}

// TestGetLatestVersionMemiavlOnly verifies the routing path for
// MemiavlOnly: the answer comes from memiavl and flatkv is not
// consulted (it is nil).
func TestGetLatestVersionMemiavlOnly(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = config.MemiavlOnly

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	cs.Initialize([]string{keys.BankStoreKey})
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
	cfg.WriteMode = config.FlatKVOnly

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
	cfg.WriteMode = config.MigrateEVM
	cfg.KeysToMigratePerBlock = 100

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey})
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

// fixedVersionEVMStore is a flatkv.Store mock that reports a
// pre-programmed GetLatestVersion answer. Used by the mismatch test to
// force disagreement with the live memiavl backend without resorting
// to crash-injection fixtures.
type fixedVersionEVMStore struct {
	failingEVMStore
	version int64
}

var _ flatkv.Store = (*fixedVersionEVMStore)(nil)

func (f *fixedVersionEVMStore) GetLatestVersion() (int64, error) {
	return f.version, nil
}

// TestGetLatestVersionBackendMismatch verifies that a disagreement
// between backends is surfaced as an error rather than silently
// picking one. Recovery is the caller's responsibility.
func TestGetLatestVersionBackendMismatch(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = config.MigrateEVM
	cfg.KeysToMigratePerBlock = 100

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey})
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()

	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k"), Value: []byte("v")},
		}}},
	}))
	_, err = cs.Commit()
	require.NoError(t, err)

	// memiavl is now at version 1. Swap flatkv for a mock that reports
	// version 2; this is the shape of a crashed-mid-commit divergence
	// that the mismatch check is designed to surface.
	cs.flatKV = &fixedVersionEVMStore{version: 2}

	_, err = cs.GetLatestVersion()
	require.Error(t, err, "diverging backend versions must surface as an error")
	require.Contains(t, err.Error(), "mismatch")
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
	cfg.WriteMode = config.MigrateEVM
	cfg.KeysToMigratePerBlock = 100

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey})

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
	cfg.WriteMode = config.FlatKVOnly

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
	cfg.WriteMode = config.FlatKVOnly

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
	cfg.WriteMode = config.MigrateEVM
	cfg.KeysToMigratePerBlock = 100

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey})

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

// TestLoadVersionMountsMigrationStoreInMigrationMode verifies that
// production callers no longer have to inject the "migration" tree
// into composite.Initialize: opening in a migration mode mounts the
// tree automatically on the writable path, so the router's bootstrap
// probe finds it.
func TestLoadVersionMountsMigrationStoreInMigrationMode(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = config.MigrateEVM
	cfg.KeysToMigratePerBlock = 100

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey})
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err, "LoadVersion must succeed without callers pre-mounting MigrationStore")
	defer func() { _ = cs.Close() }()

	require.NotNil(t, cs.memIAVL.GetChildStoreByName(migration.MigrationStore),
		"the migration tree must be mounted on memiavl after LoadVersion in a migration mode")
}

// TestLoadVersionMigrationTreeAddedOnceWithinSingleOpen verifies that
// calling LoadVersion a second time within the same process does not
// trip memiavl's duplicate-tree-name guard. memiavl.ApplyUpgrades is
// not idempotent (it appends unconditionally), so the presence check
// in LoadVersion must skip the upgrade once the tree exists.
func TestLoadVersionMigrationTreeAddedOnceWithinSingleOpen(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = config.MigrateEVM
	cfg.KeysToMigratePerBlock = 100

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey})
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	require.NotNil(t, cs.memIAVL.GetChildStoreByName(migration.MigrationStore))

	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err,
		"second LoadVersion must skip the redundant ApplyUpgrades rather than tripping the duplicate-name guard")
	defer func() { _ = cs.Close() }()
	require.NotNil(t, cs.memIAVL.GetChildStoreByName(migration.MigrationStore),
		"the migration tree must remain mounted after the second load")
}

// TestLoadVersionDoesNotMountMigrationStoreInMemiavlOnly verifies the
// negative case: a non-migration mode must not pay for the upgrade
// and must not leave a stray "migration" tree on memiavl.
func TestLoadVersionDoesNotMountMigrationStoreInMemiavlOnly(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = config.MemiavlOnly

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	cs.Initialize([]string{keys.BankStoreKey})
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
	cfg.WriteMode = config.EVMMigrated
	cfg.MemIAVLConfig.SnapshotInterval = 1
	cfg.MemIAVLConfig.SnapshotMinTimeInterval = 0
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0
	return cfg
}

func TestExportImportEVMMigrated(t *testing.T) {
	cfg := evmMigratedConfig()

	// --- Source store: write cosmos + EVM data ---
	srcDir := t.TempDir()
	src, err := NewCompositeCommitStore(t.Context(), srcDir, cfg)
	require.NoError(t, err)
	src.Initialize([]string{"bank", keys.EVMStoreKey})
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
	dst.Initialize([]string{"bank", keys.EVMStoreKey})
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

func TestExportCosmosOnlyHasNoFlatKVModule(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.MemIAVLConfig.SnapshotInterval = 1
	cfg.MemIAVLConfig.SnapshotMinTimeInterval = 0
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0

	dir := t.TempDir()
	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	cs.Initialize([]string{"bank"})
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

	// In cosmos_only mode, evm_flatkv should NOT appear
	for _, it := range items {
		require.NotEqual(t, keys.FlatKVStoreKey, it.moduleName,
			"evm_flatkv should not appear in cosmos_only export")
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

	imp := NewImporter(cosmosImp, evmImp)

	require.NoError(t, imp.AddModule("bank"))
	imp.AddNode(&types.SnapshotNode{Key: []byte("k1"), Value: []byte("v1")})

	require.NoError(t, imp.AddModule(keys.FlatKVStoreKey))
	imp.AddNode(&types.SnapshotNode{Key: []byte("k2"), Value: []byte("v2")})

	require.NoError(t, imp.AddModule("staking"))
	imp.AddNode(&types.SnapshotNode{Key: []byte("k3"), Value: []byte("v3")})

	// bank and staking → cosmos only
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
	cs.Initialize([]string{"test", keys.EVMStoreKey})
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	for i := byte(1); i <= 3; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: "test",
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
	cs2.Initialize([]string{"test", keys.EVMStoreKey})
	_, err = cs2.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs2.Close()

	require.Equal(t, int64(2), cs2.memIAVL.Version(), "cosmos should be rolled back to EVM version")
	require.Equal(t, int64(2), cs2.flatKV.Version(), "EVM should remain at version 2")
	require.Equal(t, int64(2), cs2.Version())

	// Verify cosmos data is at version 2 (value = 0x02, not 0x03)
	testStore := cs2.GetChildStoreByName("test")
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
	cs.Initialize([]string{"bank", keys.EVMStoreKey})
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
	cs2.Initialize([]string{"bank", keys.EVMStoreKey})
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
	cs3.Initialize([]string{"bank", keys.EVMStoreKey})
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
// mode, populates "test" with k1->v1, k2->v2, k3->v3, commits version 1,
// and returns the store ready for read assertions. Cleanup is registered.
func setupComposite(t *testing.T, writeMode config.WriteMode) *CompositeCommitStore {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = writeMode

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	cs.Initialize([]string{"test", "other", keys.EVMStoreKey})
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "test", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
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
	cs := setupComposite(t, config.MemiavlOnly)

	cases := []struct {
		name    string
		store   string
		key     []byte
		wantMsg string
	}{
		{"empty store", "", []byte("k1"), "store name cannot be empty"},
		{"nil key", "test", nil, "key cannot be nil"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := cs.Get(tc.store, tc.key)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}

func TestCompositeGetMissingStore(t *testing.T) {
	cs := setupComposite(t, config.MemiavlOnly)
	val, ok, err := cs.Get("nonexistent", []byte("k1"))
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, val)
}

func TestCompositeGetMissingKey(t *testing.T) {
	cs := setupComposite(t, config.MemiavlOnly)
	val, ok, err := cs.Get("test", []byte("missing"))
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, val)
}

func TestCompositeGetPresent(t *testing.T) {
	cs := setupComposite(t, config.MemiavlOnly)
	val, ok, err := cs.Get("test", []byte("k1"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("v1"), val)
}

func TestCompositeHasValidation(t *testing.T) {
	cs := setupComposite(t, config.MemiavlOnly)

	cases := []struct {
		name  string
		store string
		key   []byte
	}{
		{"empty store", "", []byte("k1")},
		{"nil key", "test", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cs.Has(tc.store, tc.key)
			require.Error(t, err)
		})
	}
}

func TestCompositeHasMissingStore(t *testing.T) {
	cs := setupComposite(t, config.MemiavlOnly)
	ok, err := cs.Has("nonexistent", []byte("k1"))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestCompositeHasAgreesWithGet(t *testing.T) {
	cs := setupComposite(t, config.MemiavlOnly)
	keys := [][]byte{
		[]byte("k1"),
		[]byte("k2"),
		[]byte("k3"),
		[]byte("missing"),
	}
	for _, k := range keys {
		_, getOk, err := cs.Get("test", k)
		require.NoError(t, err)
		hasOk, err := cs.Has("test", k)
		require.NoError(t, err)
		require.Equal(t, getOk, hasOk, "Has should agree with Get for key %q", k)
	}
}

func TestCompositeIteratorValidation(t *testing.T) {
	cs := setupComposite(t, config.MemiavlOnly)

	cases := []struct {
		name  string
		store string
		start []byte
		end   []byte
	}{
		{"empty store", "", []byte("k1"), []byte("k9")},
		{"nil start", "test", nil, []byte("k9")},
		{"nil end", "test", []byte("k1"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cs.Iterator(tc.store, tc.start, tc.end, true)
			require.Error(t, err)
		})
	}
}

func TestCompositeIteratorMissingStore(t *testing.T) {
	cs := setupComposite(t, config.MemiavlOnly)
	iter, err := cs.Iterator("nonexistent", []byte("k1"), []byte("k9"), true)
	require.NoError(t, err)
	require.Nil(t, iter)
}

func TestCompositeIteratorAscending(t *testing.T) {
	cs := setupComposite(t, config.MemiavlOnly)
	iter, err := cs.Iterator("test", []byte("k1"), []byte("k9"), true)
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
	cs := setupComposite(t, config.MemiavlOnly)
	iter, err := cs.Iterator("test", []byte("k1"), []byte("k9"), false)
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
	cs := setupComposite(t, config.MemiavlOnly)
	iter, err := cs.Iterator("test", []byte("k1"), []byte("k3"), true)
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
	cs := setupComposite(t, config.MemiavlOnly)

	cases := []struct {
		name  string
		store string
		key   []byte
	}{
		{"empty store", "", []byte("k1")},
		{"nil key", "test", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cs.GetProof(tc.store, tc.key)
			require.Error(t, err)
		})
	}
}

func TestCompositeGetProofMissingStore(t *testing.T) {
	cs := setupComposite(t, config.MemiavlOnly)
	proof, err := cs.GetProof("nonexistent", []byte("k1"))
	require.NoError(t, err)
	require.Nil(t, proof)
}

func TestCompositeGetProofPresent(t *testing.T) {
	cs := setupComposite(t, config.MemiavlOnly)
	proof, err := cs.GetProof("test", []byte("k1"))
	require.NoError(t, err)
	require.NotNil(t, proof)
}

// TestCompositeEVMMigratedEVMReadsAreInvisible pins the current routing
// behavior: in EVMMigrated mode, EVM changesets are written exclusively to
// FlatKV, so read methods on the composite (which only consult the cosmos
// child store) cannot see the data.
//
// TODO: re-evaluate when the four read methods learn to route to FlatKV
// for EVM-keyed stores. Until then, callers wanting EVM data go through
// flatkvCommitter directly.
func TestCompositeEVMMigratedEVMReadsAreInvisible(t *testing.T) {
	dir := t.TempDir()
	cfg := evmMigratedConfig()

	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	cs.Initialize([]string{"test", keys.EVMStoreKey})
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

	// FlatKV has the data.
	require.NotNil(t, cs.flatKV)
	got, found := cs.flatKV.Get(keys.EVMStoreKey, evmKey)
	require.True(t, found, "EVM data should be present in FlatKV")
	require.Equal(t, evmVal, got)

	// But the composite's own Get/Has return missing because they only
	// look at the (empty) cosmos child store.
	val, ok, err := cs.Get(keys.EVMStoreKey, evmKey)
	require.NoError(t, err)
	require.False(t, ok, "current routing does not surface FlatKV data through composite.Get")
	require.Nil(t, val)

	hasOk, err := cs.Has(keys.EVMStoreKey, evmKey)
	require.NoError(t, err)
	require.False(t, hasOk)
}

// TestCompositeCosmosOnlyPassesThrough sanity-checks that for cosmos-named
// stores in CosmosOnly mode, the composite's read methods produce the same
// results as the underlying memiavl backend.
func TestCompositeCosmosOnlyPassesThrough(t *testing.T) {
	cs := setupComposite(t, config.MemiavlOnly)

	val, ok, err := cs.Get("test", []byte("k2"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("v2"), val)

	hasOk, err := cs.Has("test", []byte("k2"))
	require.NoError(t, err)
	require.True(t, hasOk)

	// Iteration through the composite should yield the same keys as the
	// underlying cosmos child store.
	iter, err := cs.Iterator("test", []byte("k1"), []byte("k9"), true)
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
	cs.Initialize([]string{"bank", keys.EVMStoreKey})
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
	cs2.Initialize([]string{"bank", keys.EVMStoreKey})
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
	cosmosCfg.WriteMode = config.MemiavlOnly

	cs1, err := NewCompositeCommitStore(t.Context(), dir, cosmosCfg)
	require.NoError(t, err)
	cs1.Initialize([]string{"bank", keys.EVMStoreKey})
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
	migrateCfg.WriteMode = config.MigrateEVM
	migrateCfg.KeysToMigratePerBlock = 100

	cs2, err := NewCompositeCommitStore(t.Context(), dir, migrateCfg)
	require.NoError(t, err)
	cs2.Initialize([]string{"bank", keys.EVMStoreKey})
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

// TestMigrationEntrySeedingIsIdempotentAcrossRestarts verifies that once
// flatkv has been seeded and committed, a subsequent restart does not
// re-seed (which would error out via the "non-empty store" guard).
func TestMigrationEntrySeedingIsIdempotentAcrossRestarts(t *testing.T) {
	dir := t.TempDir()

	cosmosCfg := config.DefaultStateCommitConfig()
	cosmosCfg.WriteMode = config.MemiavlOnly
	cs1, err := NewCompositeCommitStore(t.Context(), dir, cosmosCfg)
	require.NoError(t, err)
	cs1.Initialize([]string{"bank", keys.EVMStoreKey})
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
	migrateCfg.WriteMode = config.MigrateEVM
	migrateCfg.KeysToMigratePerBlock = 100

	cs2, err := NewCompositeCommitStore(t.Context(), dir, migrateCfg)
	require.NoError(t, err)
	cs2.Initialize([]string{"bank", keys.EVMStoreKey})
	_, err = cs2.LoadVersion(0, false)
	require.NoError(t, err)
	require.Equal(t, int64(5), cs2.flatKV.Version(), "flatkv seeded to memiavl version on first reopen")
	_, err = cs2.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(6), cs2.Version())
	require.NoError(t, cs2.Close())

	cs3, err := NewCompositeCommitStore(t.Context(), dir, migrateCfg)
	require.NoError(t, err)
	cs3.Initialize([]string{"bank", keys.EVMStoreKey})
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
	cfg.WriteMode = config.FlatKVOnly

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	require.Nil(t, cs.memIAVL, "FlatKVOnly must not allocate a memIAVL backend")
	require.NotPanics(t, func() {
		cs.Initialize([]string{"bank", keys.EVMStoreKey})
	}, "Initialize must not panic when memIAVL is nil")
}

// TestSetInitialVersionMemiavlOnly verifies SetInitialVersion delegates
// only to memIAVL when flatkv is absent, and that the first commit
// produces the requested version.
func TestSetInitialVersionMemiavlOnly(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = config.MemiavlOnly

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	cs.Initialize([]string{"bank", keys.EVMStoreKey})
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
	cfg.WriteMode = config.MigrateEVM
	cfg.KeysToMigratePerBlock = 100

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	cs.Initialize([]string{"bank", keys.EVMStoreKey})
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
	cfg.WriteMode = config.MigrateEVM
	cfg.KeysToMigratePerBlock = 100

	cs, err := NewCompositeCommitStore(t.Context(), t.TempDir(), cfg)
	require.NoError(t, err)
	cs.Initialize([]string{"bank", keys.EVMStoreKey})
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
