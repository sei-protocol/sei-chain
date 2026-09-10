package gigasim

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfigIsValid(t *testing.T) {
	t.Parallel()
	require.NoError(t, DefaultGigasimConfig().Validate())
}

func TestShippedConfigsAreValid(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join("config", "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no shipped configs were found")

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			config := DefaultGigasimConfig()
			require.NoError(t, utils.LoadConfigFromFile(path, config))
			require.NoError(t, config.Validate())
		})
	}
}

func TestLoadingRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"NoSuchField": 1}`), 0o600))

	require.Error(t, utils.LoadConfigFromFile(path, DefaultGigasimConfig()))
}

func TestValidationRejectsUnusableValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*GigasimConfig)
	}{
		{"a block with no transactions", func(c *GigasimConfig) { c.TransactionsPerBlock = 0 }},
		{"a block larger than consensus allows", func(c *GigasimConfig) { c.TransactionsPerBlock = 5000 }},
		{"a payload larger than consensus allows", func(c *GigasimConfig) { c.BytesPerTransaction = 1 << 20 }},
		// The default block already fills the payload budget exactly, so a transaction one byte wider
		// than the default overflows it unless the block also gets shorter.
		{"a default block with wider transactions", func(c *GigasimConfig) { c.BytesPerTransaction++ }},
		{"a probability above one", func(c *GigasimConfig) { c.HotAccountProbability = 1.5 }},
		{"a negative block rate", func(c *GigasimConfig) { c.BlocksPerSecond = -1 }},
		{"a lookback window below the infinite sentinel", func(c *GigasimConfig) { c.LookbackWindow = -2 }},
		{"a prune interval of zero", func(c *GigasimConfig) { c.PruneIntervalSeconds = 0 }},
		{"a hot ERC20 set as large as the whole population", func(c *GigasimConfig) {
			c.HotErc20ContractSetSize = c.MinimumNumberOfErc20Contracts
		}},
		{"a random buffer too small for one block", func(c *GigasimConfig) { c.CannedRandomSize = 8 }},
		{"no data directory", func(c *GigasimConfig) { c.DataDir = "" }},
		{"an unknown log level", func(c *GigasimConfig) { c.LogLevel = "chatty" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := DefaultGigasimConfig()
			test.mutate(config)
			require.Error(t, config.Validate())
		})
	}
}

func TestStorageConfigCarriesTheConfiguredWindows(t *testing.T) {
	t.Parallel()

	config := DefaultGigasimConfig()
	config.DataDir = t.TempDir()
	config.RollbackWindow = 77
	config.LookbackWindow = 88
	config.PruneIntervalSeconds = 11
	config.CheckpointIntervalSeconds = 22
	config.CheckpointBlockInterval = 33

	storage, err := config.storageConfig()
	require.NoError(t, err)

	require.Equal(t, uint64(77), storage.PruningConfig.RollbackWindow)
	require.Equal(t, int64(88), storage.PruningConfig.LookbackWindow)
	require.Equal(t, 11*time.Second, storage.PruningConfig.PruneInterval)
	require.Equal(t, 22*time.Second, storage.CheckpointConfig.TimeInterval)
	require.Equal(t, int64(33), storage.CheckpointConfig.BlockInterval)
}

func TestDisablingAStoreKeepsItOutOfTheStorageConfig(t *testing.T) {
	t.Parallel()

	config := DefaultGigasimConfig()
	config.DataDir = t.TempDir()
	config.EnableStateStore = false
	config.EnableReceiptStore = false

	storage, err := config.storageConfig()
	require.NoError(t, err)

	require.False(t, storage.SSConfig.Enable)
	require.False(t, storage.ReceiptDBConfig.Enable)
}
