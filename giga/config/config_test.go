package config_test

import (
	"testing"
	"time"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/stretchr/testify/require"
)

// TestReadConfigAbsentKeysKeepDefaults pins the section baseline: no [giga] section means the node
// runs what it ran before the section existed.
func TestReadConfigAbsentKeysKeepDefaults(t *testing.T) {
	cfg, err := gigaconfig.ReadConfig(configtest.AppOpts{})
	require.NoError(t, err, "an absent [giga] section must read cleanly")
	require.Equal(t, gigaconfig.DefaultConfig, cfg)
}

// TestDefaultsMatchTheStorageDefaults holds the section's storage defaults against the values
// sei-db builds when nothing configures it.
func TestDefaultsMatchTheStorageDefaults(t *testing.T) {
	gc := seidbconfig.DefaultStorageGarbageCollectorConfig()
	cp := seidbconfig.DefaultCheckpointConfig()
	s := gigaconfig.DefaultConfig.Storage
	require.True(t, s.Receipts)
	require.Equal(t, gc.RollbackWindow, s.RollbackWindow)
	require.Equal(t, gc.LookbackWindow, s.LookbackWindow)
	require.Equal(t, gc.PruneInterval, s.PruneInterval)
	require.Equal(t, cp.TimeInterval, s.CheckpointTimeInterval)
	require.Equal(t, cp.BlockInterval, s.CheckpointBlockInterval)
}

func TestReadConfigReadsEveryKey(t *testing.T) {
	cfg, err := gigaconfig.ReadConfig(configtest.AppOpts{
		"giga.storage.mode":                      "full",
		"giga.storage.receipts":                  "false",
		"giga.storage.rollback_window":           "250",
		"giga.storage.lookback_window":           "-1",
		"giga.storage.prune_interval":            "90s",
		"giga.storage.checkpoint_time_interval":  "1h",
		"giga.storage.checkpoint_block_interval": "5000",
		"giga.execution.min_gas_price":           "7",
		"giga.execution.occ_workers":             "3",
		"giga.execution.parse_workers":           "2",
		"giga.execution.block_result_pool_size":  "4",
		"giga.execution.evm_rpc_port":            "8600",
	})
	require.NoError(t, err)
	want := gigaconfig.Config{
		Storage: gigaconfig.StorageConfig{
			Mode:                    gigaconfig.StorageModeFull,
			Receipts:                false,
			RollbackWindow:          250,
			LookbackWindow:          -1,
			PruneInterval:           90 * time.Second,
			CheckpointTimeInterval:  time.Hour,
			CheckpointBlockInterval: 5000,
		},
		Execution: gigaconfig.ExecutionConfig{
			MinGasPrice:         7,
			OCCWorkers:          3,
			ParseWorkers:        2,
			BlockResultPoolSize: 4,
			EvmRpcPort:          8600,
		},
	}
	require.Equal(t, want, cfg)
}

func TestReadConfigRejectsUnusableValues(t *testing.T) {
	for name, opts := range map[string]configtest.AppOpts{
		"unknown mode":            {"giga.storage.mode": "archive"},
		"non-boolean receipts":    {"giga.storage.receipts": "maybe"},
		"lookback below -1":       {"giga.storage.lookback_window": "-2"},
		"zero prune interval":     {"giga.storage.prune_interval": "0s"},
		"negative occ workers":    {"giga.execution.occ_workers": "-1"},
		"empty result pool":       {"giga.execution.block_result_pool_size": "0"},
		"non-numeric gas price":   {"giga.execution.min_gas_price": "cheap"},
		"negative block interval": {"giga.storage.checkpoint_block_interval": "-1"},
		"evm rpc port zero":       {"giga.execution.evm_rpc_port": "0"},
		"evm rpc port too high":   {"giga.execution.evm_rpc_port": "65536"},
	} {
		_, err := gigaconfig.ReadConfig(opts)
		require.Error(t, err, name)
	}
}
