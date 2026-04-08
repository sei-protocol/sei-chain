package rootmulti

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/stretchr/testify/require"
)

func TestCommitAndHistoricalQueryHashConsistency(t *testing.T) {
	scConfig := seidbconfig.DefaultStateCommitConfig()
	scConfig.MemIAVLConfig.AsyncCommitBuffer = 0
	scConfig.MemIAVLConfig.SnapshotMinTimeInterval = 0
	scConfig.HistoricalProofRateLimit = 0
	scConfig.HistoricalProofMaxInFlight = 100
	ssConfig := seidbconfig.StateStoreConfig{}

	store := NewStore(t.TempDir(), scConfig, ssConfig, nil)

	keys := []string{"acc", "bank", "distribution", "staking", "ibc", "upgrade"}
	storeKeys := make(map[string]*types.KVStoreKey)
	for _, name := range keys {
		sk := types.NewKVStoreKey(name)
		storeKeys[name] = sk
		store.MountStoreWithDB(sk, types.StoreTypeIAVL, nil)
	}

	require.NoError(t, store.LoadLatestVersion())

	type record struct {
		version int64
		hash    []byte
		infos   []types.StoreInfo
	}
	var records []record

	for block := 1; block <= 5; block++ {
		cms := store.CacheMultiStore()

		accStore := cms.GetKVStore(storeKeys["acc"])
		distStore := cms.GetKVStore(storeKeys["distribution"])
		stakingStore := cms.GetKVStore(storeKeys["staking"])
		bankStore := cms.GetKVStore(storeKeys["bank"])

		switch block {
		case 1:
			accStore.Set([]byte("acct1"), []byte("balance100"))
			bankStore.Set([]byte("supply"), []byte("1000"))
			distStore.Set([]byte("rewards1"), []byte("10"))
			stakingStore.Set([]byte("validator1"), []byte("power100"))
		default:
			distStore.Set([]byte("rewards1"), []byte("updated_"+string(rune('0'+block))))
			stakingStore.Set([]byte("validator1"), []byte("power_"+string(rune('0'+block))))
		}

		cms.Write()
		store.GetWorkingHash()
		cid := store.Commit(true)

		infos := make([]types.StoreInfo, len(store.lastCommitInfo.StoreInfos))
		copy(infos, store.lastCommitInfo.StoreInfos)
		records = append(records, record{
			version: cid.Version,
			hash:    cid.Hash,
			infos:   infos,
		})

		t.Logf("COMMIT ver=%d hash=%X", cid.Version, cid.Hash)
		for _, si := range infos {
			t.Logf("  %s ver=%d hash=%X", si.Name, si.CommitId.Version, si.CommitId.Hash)
		}
	}

	for _, rec := range records {
		t.Logf("--- Historical query version %d ---", rec.version)

		scStore, err := store.scStore.LoadVersion(rec.version, true)
		require.NoError(t, err)

		commitInfo := convertCommitInfo(scStore.LastCommitInfo())
		commitInfo = amendCommitInfo(commitInfo, store.storesParams)

		t.Logf("QUERY ver=%d hash=%X", commitInfo.Version, commitInfo.Hash())
		for _, si := range commitInfo.StoreInfos {
			t.Logf("  %s ver=%d hash=%X", si.Name, si.CommitId.Version, si.CommitId.Hash)
		}

		require.Equalf(t, rec.hash, commitInfo.Hash(),
			"ROOT HASH MISMATCH at version %d", rec.version)

		scStore.Close()
	}
}

func TestCommitAndHistoricalQueryWithDoubleFlush(t *testing.T) {
	scConfig := seidbconfig.DefaultStateCommitConfig()
	scConfig.MemIAVLConfig.AsyncCommitBuffer = 0
	scConfig.MemIAVLConfig.SnapshotMinTimeInterval = 0
	scConfig.HistoricalProofRateLimit = 0
	scConfig.HistoricalProofMaxInFlight = 100
	ssConfig := seidbconfig.StateStoreConfig{}

	store := NewStore(t.TempDir(), scConfig, ssConfig, nil)

	keys := []string{"acc", "bank", "distribution", "staking", "ibc", "upgrade"}
	storeKeys := make(map[string]*types.KVStoreKey)
	for _, name := range keys {
		sk := types.NewKVStoreKey(name)
		storeKeys[name] = sk
		store.MountStoreWithDB(sk, types.StoreTypeIAVL, nil)
	}

	require.NoError(t, store.LoadLatestVersion())

	type record struct {
		version int64
		hash    []byte
		infos   []types.StoreInfo
	}
	var records []record

	for block := 1; block <= 5; block++ {
		cms := store.CacheMultiStore()

		accStore := cms.GetKVStore(storeKeys["acc"])
		distStore := cms.GetKVStore(storeKeys["distribution"])
		stakingStore := cms.GetKVStore(storeKeys["staking"])
		bankStore := cms.GetKVStore(storeKeys["bank"])

		switch block {
		case 1:
			accStore.Set([]byte("acct1"), []byte("balance100"))
			bankStore.Set([]byte("supply"), []byte("1000"))
			distStore.Set([]byte("rewards1"), []byte("10"))
			stakingStore.Set([]byte("validator1"), []byte("power100"))
		default:
			distStore.Set([]byte("rewards1"), []byte("updated_"+string(rune('0'+block))))
			stakingStore.Set([]byte("validator1"), []byte("power_"+string(rune('0'+block))))
		}

		// Simulate FinalizeBlocker: Write + GetWorkingHash
		cms.Write()
		store.GetWorkingHash()

		// Simulate Commit: Write + GetWorkingHash + Commit (double flush)
		cms.Write()
		store.GetWorkingHash()
		cid := store.Commit(true)

		infos := make([]types.StoreInfo, len(store.lastCommitInfo.StoreInfos))
		copy(infos, store.lastCommitInfo.StoreInfos)
		records = append(records, record{
			version: cid.Version,
			hash:    cid.Hash,
			infos:   infos,
		})

		t.Logf("COMMIT ver=%d hash=%X", cid.Version, cid.Hash)
	}

	for _, rec := range records {
		scStore, err := store.scStore.LoadVersion(rec.version, true)
		require.NoError(t, err)

		commitInfo := convertCommitInfo(scStore.LastCommitInfo())
		commitInfo = amendCommitInfo(commitInfo, store.storesParams)

		require.Equalf(t, rec.hash, commitInfo.Hash(),
			"ROOT HASH MISMATCH at version %d", rec.version)

		scStore.Close()
	}
}
