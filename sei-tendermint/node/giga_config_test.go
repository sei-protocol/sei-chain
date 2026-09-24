package node

import (
	"math/big"
	"testing"
	"time"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestResolveGigaStorageMode(t *testing.T) {
	for _, tc := range []struct {
		nodeMode, pinned, want string
	}{
		{config.ModeValidator, gigaconfig.StorageModeAuto, gigaconfig.StorageModeValidator},
		{config.ModeFull, gigaconfig.StorageModeAuto, gigaconfig.StorageModeFull},
		{"", gigaconfig.StorageModeAuto, gigaconfig.StorageModeValidator},
		{config.ModeValidator, gigaconfig.StorageModeFull, gigaconfig.StorageModeFull},
		{config.ModeFull, gigaconfig.StorageModeValidator, gigaconfig.StorageModeValidator},
	} {
		require.Equal(t, tc.want, resolveGigaStorageMode(tc.nodeMode, tc.pinned))
	}
}

func TestBuildGigaStorageConfigDefaultsMatchAutobahnStorageConfig(t *testing.T) {
	dir := t.TempDir()
	want, err := seidbconfig.AutobahnStorageConfig(dir)
	require.NoError(t, err)
	got, err := buildGigaStorageConfig(dir, config.ModeValidator, gigaconfig.DefaultConfig.Storage)
	require.NoError(t, err)
	require.Equal(t, want.SSConfig.Enable, got.SSConfig.Enable)
	require.Equal(t, want.ReceiptDBConfig.Enable, got.ReceiptDBConfig.Enable)
	require.Equal(t, want.PruningConfig, got.PruningConfig)
	require.Equal(t, want.CheckpointConfig, got.CheckpointConfig)
}

func TestBuildGigaStorageConfigAppliesSection(t *testing.T) {
	storage := gigaconfig.StorageConfig{
		Mode:                    gigaconfig.StorageModeAuto,
		RollbackWindow:          42,
		LookbackWindow:          7,
		PruneInterval:           time.Second,
		CheckpointTimeInterval:  time.Minute,
		CheckpointBlockInterval: 9,
	}
	got, err := buildGigaStorageConfig(t.TempDir(), config.ModeFull, storage)
	require.NoError(t, err)
	require.True(t, got.SSConfig.Enable)
	require.True(t, got.ReceiptDBConfig.Enable)
	require.Equal(t, uint64(42), got.PruningConfig.RollbackWindow)
	require.Equal(t, int64(7), got.PruningConfig.LookbackWindow)
	require.Equal(t, time.Second, got.PruningConfig.PruneInterval)
	require.Equal(t, time.Minute, got.CheckpointConfig.TimeInterval)
	require.Equal(t, int64(9), got.CheckpointConfig.BlockInterval)

	got, err = buildGigaStorageConfig(t.TempDir(), config.ModeValidator, storage)
	require.NoError(t, err)
	require.False(t, got.SSConfig.Enable)
}

func TestPrepareApplicationFullModeOpensStateStore(t *testing.T) {
	validator := makeValidator([]byte("full-validator"), []byte("full-node"), "localhost:26660")
	autobahnConfigFile := writeAutobahnConfig(t, defaultFileConfig(t, []config.AutobahnValidator{validator}))

	_, storage, err := prepareApplication(t.Context(), &config.Config{
		BaseConfig:         config.BaseConfig{Mode: config.ModeFull},
		AutobahnConfigFile: autobahnConfigFile,
	}, abci.BaseApplication{}, gigaconfig.DefaultConfig)
	require.NoError(t, err)
	manager, ok := storage.Get()
	require.True(t, ok)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	require.NotNil(t, manager.SS())
}

func TestPrepareApplicationEVMOnlyUsesExecutionConfig(t *testing.T) {
	validator := makeValidator([]byte("exec-validator"), []byte("exec-node"), "localhost:26660")
	autobahnConfigFile := writeAutobahnConfig(t, defaultFileConfig(t, []config.AutobahnValidator{validator}))
	giga := gigaconfig.DefaultConfig
	giga.Execution.MinGasPrice = 2_000_000_000

	prepared, storage, err := prepareApplication(t.Context(), &config.Config{
		BaseConfig:         config.BaseConfig{FastCheckTx: true},
		AutobahnConfigFile: autobahnConfigFile,
	}, abci.BaseApplication{}, giga)
	require.NoError(t, err)
	manager, ok := storage.Get()
	require.True(t, ok)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })

	provider, ok := prepared.(interface{ EvmMinGasPrice() *big.Int })
	require.True(t, ok)
	require.Equal(t, 0, provider.EvmMinGasPrice().Cmp(big.NewInt(2_000_000_000)))
}
