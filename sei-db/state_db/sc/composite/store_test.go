package composite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

func TestCompositeStoreBasicOperations(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs := NewCompositeCommitStore(t.Context(), dir, logger.NewNopLogger(), cfg)
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

	cs := NewCompositeCommitStore(t.Context(), dir, logger.NewNopLogger(), cfg)
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

	cs := NewCompositeCommitStore(t.Context(), dir, logger.NewNopLogger(), cfg)
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

	cs := NewCompositeCommitStore(t.Context(), dir, logger.NewNopLogger(), cfg)
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

func TestEnableLatticeHash(t *testing.T) {
	evmStorageKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage,
		append(make([]byte, 20), make([]byte, 32)...))

	makeChangesets := func() []*proto.NamedChangeSet {
		return []*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("key"), Value: []byte("value")},
					},
				},
			},
			{
				Name: EVMStoreName,
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: evmStorageKey, Value: []byte{0x42}},
					},
				},
			},
		}
	}

	t.Run("disabled uses cosmos-only hash", func(t *testing.T) {
		dir := t.TempDir()
		cfg := config.DefaultStateCommitConfig()
		cfg.WriteMode = config.DualWrite
		cfg.EnableLatticeHash = false

		cs := NewCompositeCommitStore(t.Context(), dir, logger.NewNopLogger(), cfg)
		cs.Initialize([]string{"test", EVMStoreName})
		_, err := cs.LoadVersion(0, false)
		require.NoError(t, err)
		defer cs.Close()

		require.NoError(t, cs.ApplyChangeSets(makeChangesets()))

		// Compute expected hashes from cosmos committer
		expectedWorking := cs.cosmosCommitter.WorkingCommitInfo()

		workingInfo := cs.WorkingCommitInfo()
		require.Equal(t, expectedWorking.Version, workingInfo.Version)
		require.Equal(t, len(expectedWorking.StoreInfos), len(workingInfo.StoreInfos))
		for i, si := range expectedWorking.StoreInfos {
			require.Equal(t, si.Name, workingInfo.StoreInfos[i].Name)
			require.Equal(t, si.CommitId.Hash, workingInfo.StoreInfos[i].CommitId.Hash)
		}

		_, err = cs.Commit()
		require.NoError(t, err)

		expectedLast := cs.cosmosCommitter.LastCommitInfo()

		lastInfo := cs.LastCommitInfo()
		require.Equal(t, expectedLast.Version, lastInfo.Version)
		require.Equal(t, len(expectedLast.StoreInfos), len(lastInfo.StoreInfos))
		for i, si := range expectedLast.StoreInfos {
			require.Equal(t, si.Name, lastInfo.StoreInfos[i].Name)
			require.Equal(t, si.CommitId.Hash, lastInfo.StoreInfos[i].CommitId.Hash)
		}
	})

	t.Run("enabled appends flatkv lattice hash", func(t *testing.T) {
		dir := t.TempDir()
		cfg := config.DefaultStateCommitConfig()
		cfg.WriteMode = config.DualWrite
		cfg.EnableLatticeHash = true

		cs := NewCompositeCommitStore(t.Context(), dir, logger.NewNopLogger(), cfg)
		cs.Initialize([]string{"test", EVMStoreName})
		_, err := cs.LoadVersion(0, false)
		require.NoError(t, err)
		defer cs.Close()

		require.NoError(t, cs.ApplyChangeSets(makeChangesets()))

		// Compute expected hashes before calling composite methods
		expectedCosmosWorking := cs.cosmosCommitter.WorkingCommitInfo()
		expectedEvmWorkingHash := cs.evmCommitter.RootHash()
		require.Len(t, expectedEvmWorkingHash, 32)

		workingInfo := cs.WorkingCommitInfo()
		require.Equal(t, len(expectedCosmosWorking.StoreInfos)+1, len(workingInfo.StoreInfos))
		for i, si := range expectedCosmosWorking.StoreInfos {
			require.Equal(t, si.Name, workingInfo.StoreInfos[i].Name)
			require.Equal(t, si.CommitId.Hash, workingInfo.StoreInfos[i].CommitId.Hash)
		}
		latticeEntry := workingInfo.StoreInfos[len(workingInfo.StoreInfos)-1]
		require.Equal(t, "evm", latticeEntry.Name)
		require.Equal(t, expectedEvmWorkingHash, latticeEntry.CommitId.Hash)
		require.Equal(t, workingInfo.Version, latticeEntry.CommitId.Version)

		_, err = cs.Commit()
		require.NoError(t, err)

		// Compute expected committed hashes
		expectedCosmosLast := cs.cosmosCommitter.LastCommitInfo()
		expectedEvmCommittedHash := cs.evmCommitter.CommittedRootHash()
		require.Len(t, expectedEvmCommittedHash, 32)
		require.Equal(t, expectedEvmWorkingHash, expectedEvmCommittedHash,
			"committed hash should equal working hash after commit")

		lastInfo := cs.LastCommitInfo()
		require.Equal(t, len(expectedCosmosLast.StoreInfos)+1, len(lastInfo.StoreInfos))
		for i, si := range expectedCosmosLast.StoreInfos {
			require.Equal(t, si.Name, lastInfo.StoreInfos[i].Name)
			require.Equal(t, si.CommitId.Hash, lastInfo.StoreInfos[i].CommitId.Hash)
		}
		lastLatticeEntry := lastInfo.StoreInfos[len(lastInfo.StoreInfos)-1]
		require.Equal(t, "evm", lastLatticeEntry.Name)
		require.Equal(t, expectedEvmCommittedHash, lastLatticeEntry.CommitId.Hash)
		require.Equal(t, lastInfo.Version, lastLatticeEntry.CommitId.Version)
	})
}

func TestRollback(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs := NewCompositeCommitStore(t.Context(), dir, logger.NewNopLogger(), cfg)
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

	cs := NewCompositeCommitStore(t.Context(), dir, logger.NewNopLogger(), cfg)
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

	cs2 := NewCompositeCommitStore(t.Context(), dir, logger.NewNopLogger(), cfg)
	cs2.Initialize([]string{"test"})

	latestVersion, err := cs2.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(3), latestVersion)
}
