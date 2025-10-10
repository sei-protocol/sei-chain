package rootmulti

import (
	"testing"

	"time"

	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/sei-protocol/sei-db/config"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
)

func TestLastCommitID(t *testing.T) {
	store := NewStore(t.TempDir(), log.NewNopLogger(), config.StateCommitConfig{}, config.StateStoreConfig{}, false)
	require.Equal(t, types.CommitID{}, store.LastCommitID())
}

// waitUntilSSVersion waits until the SS latest version reaches at least target or times out.
func waitUntilSSVersion(t *testing.T, store *Store, target int64) {
	ss := store.GetStateStore()
	require.NotNil(t, ss)
	require.Eventually(t, func() bool {
		return ss.GetLatestVersion() >= target
	}, 10*time.Second, 10*time.Millisecond)
}

func TestSCSS_WriteAndHistoricalRead(t *testing.T) {
	// Enable both SC and SS with default configs (pebbledb backend, async writes)
	home := t.TempDir()
	scCfg := config.DefaultStateCommitConfig()
	scCfg.Enable = true
	ssCfg := config.DefaultStateStoreConfig()
	ssCfg.Enable = true

	store := NewStore(home, log.NewNopLogger(), scCfg, ssCfg, false)
	defer func() { _ = store.Close() }()

	// Mount one IAVL store and load
	key := types.NewKVStoreKey("store1")
	store.MountStoreWithDB(key, types.StoreTypeIAVL, nil)
	require.NoError(t, store.LoadLatestVersion())

	// Write v1 and commit
	kv := store.GetStoreByName("store1").(types.KVStore)
	keyBytes := []byte("k")
	valV1 := []byte("v1")
	kv.Set(keyBytes, valV1)
	c1 := store.Commit(true)
	require.Equal(t, int64(1), c1.Version)

	// Re-acquire KV store after commit to ensure we write to the current instance
	kv = store.GetStoreByName("store1").(types.KVStore)
	// Write v2 and commit
	valV2 := []byte("v2")
	kv.Set(keyBytes, valV2)
	c2 := store.Commit(true)
	require.Equal(t, int64(2), c2.Version)

	// Current read (latest) should be v2
	cmsLatest, err := store.CacheMultiStoreWithVersion(c2.Version)
	require.NoError(t, err)
	gotLatest := cmsLatest.GetKVStore(key).Get(keyBytes)
	require.Equal(t, valV2, gotLatest)

	// Wait for SS to asynchronously catch up to v2
	waitUntilSSVersion(t, store, c2.Version)

	// Historical read at v1 should return v1 (served by SS)
	cmsV1, err := store.CacheMultiStoreWithVersion(c1.Version)
	require.NoError(t, err)
	gotV1 := cmsV1.GetKVStore(key).Get(keyBytes)
	require.Equal(t, valV1, gotV1)

	// Query API without proof at v1 should be served by SS and return v1
	resp := store.Query(abci.RequestQuery{
		Path:   "/store1/key",
		Data:   keyBytes,
		Height: c1.Version,
		Prove:  false,
	})
	require.EqualValues(t, 0, resp.Code)
	require.Equal(t, valV1, resp.Value)

	// Query API with proof at v1 should still return v1 (served by SC historical)
	resp = store.Query(abci.RequestQuery{
		Path:   "/store1/key",
		Data:   keyBytes,
		Height: c1.Version,
		Prove:  true,
	})
	require.EqualValues(t, 0, resp.Code)
	require.Equal(t, valV1, resp.Value)
}
