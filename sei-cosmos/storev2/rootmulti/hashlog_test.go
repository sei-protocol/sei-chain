package rootmulti

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

// End-to-end: committing blocks through rootmulti produces a per-block hash log on disk with every
// expected column (app/block/state hashes + changeset) populated, and the recorded values match the
// store's own commit hashes and the supplied block hashes.
func TestRootMultiHashLogging(t *testing.T) {
	home := t.TempDir()
	scCfg := config.DefaultStateCommitConfig()
	scCfg.Enable = true
	scCfg.HashLogger.Enable = true
	scCfg.HashLogger.Version = "test-v1"
	ssCfg := config.DefaultStateStoreConfig()
	ssCfg.Enable = false

	store := NewStore(home, scCfg, ssCfg, []string{})
	store.MountStoreWithDB(types.NewKVStoreKey("bank"), types.StoreTypeIAVL, nil)
	store.MountStoreWithDB(types.NewKVStoreKey("evm"), types.StoreTypeIAVL, nil)
	require.NoError(t, store.LoadLatestVersion())

	blockHashes := map[int64][]byte{}
	resultHashes := map[int64][]byte{}
	for h := int64(1); h <= 3; h++ {
		store.GetStoreByName("bank").(types.KVStore).Set([]byte("k"), []byte{byte(h)})
		store.GetStoreByName("evm").(types.KVStore).Set([]byte("k"), []byte{byte(h + 10)})
		blockHash := []byte{0xBB, byte(h)}
		blockHashes[h] = blockHash
		store.SetNextBlockHash(blockHash)
		resultHash := []byte{0xCC, byte(h)}
		resultHashes[h] = resultHash
		store.SetNextResultHash(resultHash)
		commitID := store.Commit(true)
		require.Equal(t, h, commitID.Version)
	}
	lastAppHash := store.LastCommitID().Hash

	// Close flushes the logger so all complete blocks are written.
	require.NoError(t, store.Close())

	dir := filepath.Join(home, "data", "hash.log")
	expectedColumns := []string{
		"appHash", "blockHash", "resultHash", "memIAVL/root",
		"memIAVL/mod/bank", "memIAVL/mod/evm", hashlog.ChangesetHashType,
	}
	for h := int64(1); h <= 3; h++ {
		logs, err := hashlog.ReadHashForBlock(dir, uint64(h))
		require.NoError(t, err)
		require.Len(t, logs, 1, "exactly one record per block")
		hashes := logs[0].Hashes
		for _, column := range expectedColumns {
			require.Contains(t, hashes, column)
			require.NotEmpty(t, hashes[column], "column %q for block %d should be populated", column, h)
		}
		require.Equal(t, blockHashes[h], hashes["blockHash"], "block hash for block %d", h)
		require.Equal(t, resultHashes[h], hashes["resultHash"], "result hash for block %d", h)
	}

	// The last block's appHash column equals the store's committed app hash.
	logs, err := hashlog.ReadHashForBlock(dir, 3)
	require.NoError(t, err)
	require.Equal(t, lastAppHash, logs[0].Hashes["appHash"])
}

// When hash logging is disabled, no hash log directory/files are produced and commits still succeed.
func TestRootMultiHashLoggingDisabled(t *testing.T) {
	home := t.TempDir()
	scCfg := config.DefaultStateCommitConfig()
	scCfg.Enable = true
	scCfg.HashLogger.Enable = false
	ssCfg := config.DefaultStateStoreConfig()
	ssCfg.Enable = false

	store := NewStore(home, scCfg, ssCfg, []string{})
	store.MountStoreWithDB(types.NewKVStoreKey("bank"), types.StoreTypeIAVL, nil)
	require.NoError(t, store.LoadLatestVersion())

	store.GetStoreByName("bank").(types.KVStore).Set([]byte("k"), []byte("v"))
	store.SetNextBlockHash([]byte{0x01}) // no-op when disabled
	require.Equal(t, int64(1), store.Commit(true).Version)
	require.NoError(t, store.Close())

	logs, err := hashlog.ReadHashForBlock(filepath.Join(home, "data", "hash.log"), 1)
	// Either the directory does not exist (error) or there are no records.
	if err == nil {
		require.Empty(t, logs)
	}
}
