package config

import (
	"path/filepath"
	"testing"

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
