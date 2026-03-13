package composite

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

// failingEVMStore is a mock flatkv.Store whose LoadVersion always fails.
type failingEVMStore struct{}

var _ flatkv.Store = (*failingEVMStore)(nil)

func (f *failingEVMStore) LoadVersion(int64, bool) (flatkv.Store, error) {
	return nil, fmt.Errorf("flatkv unavailable")
}
func (f *failingEVMStore) ApplyChangeSets([]*proto.NamedChangeSet) error { return nil }
func (f *failingEVMStore) Commit() (int64, error)                        { return 0, nil }
func (f *failingEVMStore) Get([]byte) ([]byte, bool)                     { return nil, false }
func (f *failingEVMStore) Has([]byte) bool                               { return false }
func (f *failingEVMStore) Iterator(_, _ []byte) flatkv.Iterator          { return nil }
func (f *failingEVMStore) IteratorByPrefix([]byte) flatkv.Iterator       { return nil }
func (f *failingEVMStore) RootHash() []byte                              { return nil }
func (f *failingEVMStore) Version() int64                                { return 0 }
func (f *failingEVMStore) WriteSnapshot(string) error                    { return nil }
func (f *failingEVMStore) Rollback(int64) error                          { return nil }
func (f *failingEVMStore) Exporter(int64) (types.Exporter, error)        { return nil, nil }
func (f *failingEVMStore) Importer(int64) (types.Importer, error)        { return nil, nil }
func (f *failingEVMStore) GetPhaseTimer() *metrics.PhaseTimer            { return nil }
func (f *failingEVMStore) Close() error                                  { return nil }

func TestCompositeStoreBasicOperations(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs.Initialize([]string{"test", EVMStoreName})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, cs.Close())
	}()

	require.Equal(t, int64(0), cs.Version())

	// Apply changesets with both regular and EVM data
	changesets := []*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key1"), Value: []byte("value1")},
				},
			},
		},
		{
			Name: EVMStoreName,
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
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

	evmStore := cs.GetChildStoreByName(EVMStoreName)
	require.NotNil(t, evmStore)
}

func TestEmptyChangesets(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
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

	cs := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
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

	cs := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, cs.Close())
	}()

	workingInfo := cs.WorkingCommitInfo()
	require.NotNil(t, workingInfo)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
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
	evmStorageKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, append(addr[:], slot[:]...))

	makeChangesets := func(round byte) []*proto.NamedChangeSet {
		return []*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("key"), Value: []byte{round}},
					},
				},
			},
			{
				Name: EVMStoreName,
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: evmStorageKey, Value: []byte{round}},
					},
				},
			},
		}
	}

	tests := []struct {
		name          string
		writeMode     config.WriteMode
		enableLattice bool
		expectLattice bool
	}{
		{"CosmosOnly/lattice_off", config.CosmosOnlyWrite, false, false},
		{"CosmosOnly/lattice_on", config.CosmosOnlyWrite, true, false},
		{"DualWrite/lattice_off", config.DualWrite, false, false},
		{"DualWrite/lattice_on", config.DualWrite, true, true},
		{"SplitWrite/lattice_off", config.SplitWrite, false, false},
		{"SplitWrite/lattice_on", config.SplitWrite, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := config.DefaultStateCommitConfig()
			cfg.WriteMode = tt.writeMode
			cfg.EnableLatticeHash = tt.enableLattice

			cs := NewCompositeCommitStore(t.Context(), dir, logger.NewNopLogger(), cfg)
			cs.Initialize([]string{"test", EVMStoreName})
			_, err := cs.LoadVersion(0, false)
			require.NoError(t, err)
			defer cs.Close()

			var prevLastHash []byte

			for round := byte(1); round <= 3; round++ {
				require.NoError(t, cs.ApplyChangeSets(makeChangesets(round)))

				// --- Working commit info ---
				expectedCosmos := cs.cosmosCommitter.WorkingCommitInfo()
				var expectedEvmHash []byte
				if tt.expectLattice {
					expectedEvmHash = cs.evmCommitter.RootHash()
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
				expectedCosmosLast := cs.cosmosCommitter.LastCommitInfo()
				var expectedEvmCommitted []byte
				if tt.expectLattice {
					expectedEvmCommitted = cs.evmCommitter.CommittedRootHash()
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

	cs := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Commit a few versions
	for i := 0; i < 3; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
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

	cs := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
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

	cs2 := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs2.Initialize([]string{"test"})

	latestVersion, err := cs2.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(3), latestVersion)
}

func TestReadOnlyLoadVersionSoftFailsWhenFlatKVUnavailable(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0

	cs := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key1"), Value: []byte("value1")},
				},
			},
		},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)

	// Inject a failing EVM committer to simulate FlatKV being unavailable
	// for historical versions (different retention, late enablement, etc).
	cs.evmCommitter = &failingEVMStore{}

	readOnly, err := cs.LoadVersion(0, true)
	require.NoError(t, err, "readonly LoadVersion should succeed even when FlatKV fails")
	defer func() { _ = readOnly.Close() }()

	compositeRO, ok := readOnly.(*CompositeCommitStore)
	require.True(t, ok)
	require.Nil(t, compositeRO.evmCommitter, "evmCommitter should be nil when FlatKV failed")

	// Cosmos data should still be accessible
	store := compositeRO.GetChildStoreByName("test")
	require.NotNil(t, store)
	val := store.Get([]byte("key1"))
	require.Equal(t, []byte("value1"), val)
}
