package cryptosim

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

func TestLoadConfigFromFile_StateStoreConfigOverridePreservesBenchmarkDefaults(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "cryptosim.json")
	err := os.WriteFile(configPath, []byte(`{
  "Backend": "SSComposite",
  "StateStoreConfig": {
    "Backend": "rocksdb"
  },
  "DataDir": "data",
  "LogDir": "logs"
}`), 0o600)
	require.NoError(t, err)

	cfg, err := LoadConfigFromFile(configPath)
	require.NoError(t, err)
	require.Equal(t, wrappers.SSComposite, cfg.Backend)
	require.Equal(t, config.RocksDBBackend, cfg.StateStoreConfig.Backend)
	require.Equal(t, config.DefaultSSAsyncBuffer, cfg.StateStoreConfig.AsyncWriteBuffer)
	require.True(t, cfg.StateStoreConfig.EVMSplit)
}

func TestLoadConfigFromFile_InvalidStateStoreBackend(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "cryptosim.json")
	err := os.WriteFile(configPath, []byte(`{
  "StateStoreConfig": {
    "Backend": "badgerdb"
  },
  "DataDir": "data",
  "LogDir": "logs"
}`), 0o600)
	require.NoError(t, err)

	_, err = LoadConfigFromFile(configPath)
	require.ErrorContains(t, err, `StateStoreConfig.Backend must be one of "pebbledb" or "rocksdb"`)
}

func TestLoadConfigFromFile_DisableTransactionReadsOverride(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "cryptosim.json")
	err := os.WriteFile(configPath, []byte(`{
  "Backend": "NoOp",
  "DisableTransactionReads": true,
  "DataDir": "data",
  "LogDir": "logs"
}`), 0o600)
	require.NoError(t, err)

	cfg, err := LoadConfigFromFile(configPath)
	require.NoError(t, err)
	require.Equal(t, wrappers.NoOp, cfg.Backend)
	require.True(t, cfg.DisableTransactionReads)
}
