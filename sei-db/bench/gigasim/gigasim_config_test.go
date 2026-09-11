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

// TestGenerationIsUnthrottledByDefault pins that a measured run is bounded by the stack rather than by
// a rate chosen in advance. Only the debug config throttles, and it says so explicitly.
func TestGenerationIsUnthrottledByDefault(t *testing.T) {
	t.Parallel()
	require.Zero(t, DefaultGigasimConfig().MaxBlocksPerSecond)
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

// TestShippedConfigsUseSeparateDirectories pins that no shipped config can destroy another's run. The
// debug config cleans its directories at both ends, so sharing a path with the standard config would
// make a smoke test delete a long benchmark's data.
func TestShippedConfigsUseSeparateDirectories(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join("config", "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no shipped configs were found")

	owners := map[string]string{}
	for _, path := range paths {
		config := DefaultGigasimConfig()
		require.NoError(t, utils.LoadConfigFromFile(path, config))
		for _, dir := range []string{config.DataDir, config.LogDir} {
			require.NotContains(t, owners, dir,
				"%s and %s both use %s", filepath.Base(path), owners[dir], dir)
			owners[dir] = filepath.Base(path)
		}
	}
}

// TestCannedRandomSizeCoversTheLargestDraw pins that validation rejects a buffer too small for a
// single contract. The buffer panics on a draw it cannot serve, and setup draws a whole contract, so
// validating against the block payload alone lets a config through that crashes on startup.
func TestCannedRandomSizeCoversTheLargestDraw(t *testing.T) {
	t.Parallel()

	config := DefaultGigasimConfig()
	config.TransactionsPerBlock = 10
	config.BytesPerTransaction = 64
	config.CannedRandomSize = config.Erc20ContractSize - 1
	require.Greater(t, config.CannedRandomSize, config.blockPayloadBytes(),
		"the buffer must clear the block payload, so that only the contract draw can reject it")

	require.ErrorContains(t, config.Validate(), "CannedRandomSize")
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
		{"a negative block rate", func(c *GigasimConfig) { c.MaxBlocksPerSecond = -1 }},
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
	config.EnableSS = false
	config.EnableReceiptStore = false

	storage, err := config.storageConfig()
	require.NoError(t, err)

	require.False(t, storage.SSConfig.Enable)
	require.False(t, storage.ReceiptDBConfig.Enable)
}
