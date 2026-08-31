package config

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
)

// TestDefaultGigaStorageConfigDirectories pins Giga's EVM-only SS layout: the path
// for evm.NewEVMStateStore lives on EVMDBDirectory. Giga does not use composite, so
// DBDirectory stays empty and EVMSplit stays at its default.
func TestDefaultGigaStorageConfigDirectories(t *testing.T) {
	home := t.TempDir()
	cfg, err := DefaultGigaStorageConfig(home)
	require.NoError(t, err)

	require.Equal(t, home, cfg.HomePath)
	require.False(t, cfg.SSConfig.EVMSplit)
	require.Empty(t, cfg.SSConfig.DBDirectory)
	require.Equal(t, utils.GetEVMStateStorePath(home, cfg.SSConfig.Backend), cfg.SSConfig.EVMDBDirectory)

	require.Equal(t, utils.GetFlatKVPath(home), cfg.FlatKVConfig.DataDir)
	require.Equal(t, utils.GetReceiptStorePath(home, cfg.ReceiptDBConfig.Backend), cfg.ReceiptDBConfig.DBDirectory)
	require.NotNil(t, cfg.BlockDBConfig.Litt)
	require.Equal(t, []string{utils.GetBlockStorePath(home)}, cfg.BlockDBConfig.Litt.Paths)

	require.Equal(t,
		filepath.Join(home, "data", "state_store", "evm", cfg.SSConfig.Backend),
		cfg.SSConfig.EVMDBDirectory,
	)
}

func defaultGigaConfig(t *testing.T) GigaStorageConfig {
	t.Helper()
	cfg, err := DefaultGigaStorageConfig(t.TempDir())
	require.NoError(t, err)
	return cfg
}

func TestTheDefaultConfigValidates(t *testing.T) {
	require.NoError(t, defaultGigaConfig(t).Validate())
}

// TestTheStoreConfigsAStoreCannotSupplyAreRequired covers the two configs the manager passes straight
// to a constructor that dereferences them: a nil one panics rather than failing.
func TestTheStoreConfigsAStoreCannotSupplyAreRequired(t *testing.T) {
	cfg := defaultGigaConfig(t)
	cfg.FlatKVConfig = nil
	require.ErrorContains(t, cfg.Validate(), "flatkv config is required")

	cfg = defaultGigaConfig(t)
	cfg.BlockDBConfig = nil
	require.ErrorContains(t, cfg.Validate(), "block db config is required")
}

func TestAnInvalidStoreConfigIsReportedByItsOwnValidate(t *testing.T) {
	cfg := defaultGigaConfig(t)
	cfg.BlockDBConfig.RetentionTime = 0
	require.ErrorContains(t, cfg.Validate(), "block db config is invalid")

	cfg = defaultGigaConfig(t)
	cfg.PruningConfig.PruneInterval = 0
	require.ErrorContains(t, cfg.Validate(), "pruning config is invalid")
}

// TestTheDirectoriesAStoreDerivesFromAreRequired covers the two paths that are not rejected downstream:
// an empty one puts a database under the working directory instead of the home directory.
func TestTheDirectoriesAStoreDerivesFromAreRequired(t *testing.T) {
	cfg := defaultGigaConfig(t)
	cfg.FlatKVConfig.DataDir = ""
	require.ErrorContains(t, cfg.Validate(), "flatkv data dir is required")

	cfg = defaultGigaConfig(t)
	cfg.SSConfig.EVMDBDirectory = ""
	require.ErrorContains(t, cfg.Validate(), "state store EVM db directory is required")
}

// TestTheDefaultFlatKVConfigIsNotValidatedAsWritten records why Validate checks DataDir rather than
// calling FlatKVConfig.Validate: the commit store resolves the nested database directories from DataDir
// first, so the config Giga hands it does not pass that check until it has.
func TestTheDefaultFlatKVConfigIsNotValidatedAsWritten(t *testing.T) {
	cfg := defaultGigaConfig(t)

	require.NoError(t, cfg.Validate())
	require.Error(t, cfg.FlatKVConfig.Validate())
}

func TestExternalPruningWithoutACollectorIsRefused(t *testing.T) {
	cfg := defaultGigaConfig(t)
	cfg.PruningConfig = nil

	err := cfg.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "state commit")
	require.ErrorContains(t, err, "EVM state store")
	require.ErrorContains(t, err, "receipt store")
}

// TestADisabledReceiptStoreStrandsNoRetention pins that receipts left external-pruned in a config that
// disables them is not the stranded case: a store that never opens grows nothing.
func TestADisabledReceiptStoreStrandsNoRetention(t *testing.T) {
	cfg := defaultGigaConfig(t)
	cfg.PruningConfig = nil
	cfg.FlatKVConfig.ExternalPruning = false
	cfg.SSConfig.ExternalPruning = false
	cfg.ReceiptDBConfig.Enable = false

	require.True(t, cfg.ReceiptDBConfig.ExternalPruning)
	require.NoError(t, cfg.Validate())
}

// TestAScheduleThatPicksNoHeightIsRefused covers the config that reads as merely conservative and is
// not: the schedule supersedes each store's own interval, so an empty one snapshots nothing at all.
func TestAScheduleThatPicksNoHeightIsRefused(t *testing.T) {
	cfg := defaultGigaConfig(t)
	cfg.CheckpointConfig = CheckpointConfig{}

	require.ErrorContains(t, cfg.Validate(), "checkpoint config must set a time interval")

	cfg.CheckpointConfig.BlockInterval = 1_000
	require.NoError(t, cfg.Validate())

	cfg.CheckpointConfig = CheckpointConfig{TimeInterval: time.Minute}
	require.NoError(t, cfg.Validate())
}
