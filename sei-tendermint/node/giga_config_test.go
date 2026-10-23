package node

import (
	"encoding/binary"
	"math/big"
	"testing"
	"time"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func genesisAt(initialHeight int64) *types.GenesisDoc {
	return &types.GenesisDoc{InitialHeight: initialHeight}
}

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
	got, err := buildGigaStorageConfig(dir, config.ModeValidator, gigaconfig.DefaultConfig.Storage, genesisAt(1))
	require.NoError(t, err)
	require.Equal(t, want.SSConfig.Enable, got.SSConfig.Enable)
	require.Equal(t, want.ReceiptDBConfig.Enable, got.ReceiptDBConfig.Enable)
	require.Equal(t, want.PruningConfig, got.PruningConfig)
	require.Equal(t, want.CheckpointConfig, got.CheckpointConfig)
	require.Equal(t, uint64(1), got.AppHashBuilderConfig.GigaActivationHeight)

	higher, err := buildGigaStorageConfig(t.TempDir(), config.ModeValidator, gigaconfig.DefaultConfig.Storage, genesisAt(5))
	require.NoError(t, err)
	require.Equal(t, uint64(5), higher.AppHashBuilderConfig.GigaActivationHeight)
	_, err = buildGigaStorageConfig(t.TempDir(), config.ModeValidator, gigaconfig.DefaultConfig.Storage, genesisAt(0))
	require.Error(t, err)
}

func TestBuildGigaStorageConfigAppliesSection(t *testing.T) {
	storage := gigaconfig.StorageConfig{
		Mode:                    gigaconfig.StorageModeAuto,
		Receipts:                true,
		RollbackWindow:          42,
		LookbackWindow:          7,
		StateStoreBackend:       seidbconfig.PebbleDBUndoBackend,
		PruneInterval:           time.Second,
		CheckpointTimeInterval:  time.Minute,
		CheckpointBlockInterval: 9,
	}
	got, err := buildGigaStorageConfig(t.TempDir(), config.ModeFull, storage, genesisAt(1))
	require.NoError(t, err)
	require.True(t, got.SSConfig.Enable)
	require.True(t, got.ReceiptDBConfig.Enable)
	require.Equal(t, uint64(42), got.PruningConfig.RollbackWindow)
	require.Equal(t, int64(7), got.PruningConfig.LookbackWindow)
	require.Equal(t, seidbconfig.PebbleDBUndoBackend, got.SSConfig.Backend)
	require.Equal(t, time.Second, got.PruningConfig.PruneInterval)
	require.Equal(t, time.Minute, got.CheckpointConfig.TimeInterval)
	require.Equal(t, int64(9), got.CheckpointConfig.BlockInterval)

	got, err = buildGigaStorageConfig(t.TempDir(), config.ModeValidator, storage, genesisAt(1))
	require.NoError(t, err)
	require.False(t, got.SSConfig.Enable)
	require.True(t, got.ReceiptDBConfig.Enable)

	storage.Receipts = false
	got, err = buildGigaStorageConfig(t.TempDir(), config.ModeFull, storage, genesisAt(1))
	require.NoError(t, err)
	require.True(t, got.SSConfig.Enable)
	require.False(t, got.ReceiptDBConfig.Enable)
	require.NoError(t, got.Validate())
}

func TestPrepareApplicationFullModeOpensStateStore(t *testing.T) {
	validator := makeValidator([]byte("full-validator"), []byte("full-node"), "localhost:26660")
	autobahnConfigFile := writeAutobahnConfig(t, defaultFileConfig(t, []config.AutobahnValidator{validator}))

	_, storage, err := prepareApplication(t.Context(), &config.Config{
		BaseConfig:         config.BaseConfig{Mode: config.ModeFull},
		AutobahnConfigFile: autobahnConfigFile,
	}, abci.BaseApplication{}, gigaconfig.DefaultConfig, genesisAt(1))
	require.NoError(t, err)
	manager, ok := storage.Get()
	require.True(t, ok)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	require.NotNil(t, manager.SS())
}

func TestPrepareApplicationReceiptsOffOpensNoReceiptStore(t *testing.T) {
	validator := makeValidator([]byte("receipts-validator"), []byte("receipts-node"), "localhost:26660")
	autobahnConfigFile := writeAutobahnConfig(t, defaultFileConfig(t, []config.AutobahnValidator{validator}))
	giga := gigaconfig.DefaultConfig
	giga.Storage.Receipts = false

	_, storage, err := prepareApplication(t.Context(), &config.Config{
		BaseConfig:         config.BaseConfig{Mode: config.ModeValidator},
		AutobahnConfigFile: autobahnConfigFile,
	}, abci.BaseApplication{}, giga, genesisAt(1))
	require.NoError(t, err)
	manager, ok := storage.Get()
	require.True(t, ok)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	require.Nil(t, manager.ReceiptDB())
}

func TestPrepareApplicationEVMOnlyUsesExecutionConfig(t *testing.T) {
	validator := makeValidator([]byte("exec-validator"), []byte("exec-node"), "localhost:26660")
	autobahnConfigFile := writeAutobahnConfig(t, defaultFileConfig(t, []config.AutobahnValidator{validator}))
	giga := gigaconfig.DefaultConfig
	giga.Execution.MinGasPrice = 2_000_000_000

	prepared, storage, err := prepareApplication(t.Context(), &config.Config{
		BaseConfig:         config.BaseConfig{FastCheckTx: true},
		AutobahnConfigFile: autobahnConfigFile,
	}, abci.BaseApplication{}, giga, genesisAt(1))
	require.NoError(t, err)
	manager, ok := storage.Get()
	require.True(t, ok)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })

	provider, ok := prepared.(interface{ EvmMinGasPrice() *big.Int })
	require.True(t, ok)
	require.Equal(t, 0, provider.EvmMinGasPrice().Cmp(big.NewInt(2_000_000_000)))
}

// The builder's first block is the genesis height. A chain that starts above 1
// still publishes the app hash of that first block.
func TestFirstAppHashFollowsTheGenesisHeight(t *testing.T) {
	const initialHeight int64 = 5
	validator := makeValidator([]byte("activation-validator"), []byte("activation-node"), "localhost:26660")
	autobahnConfigFile := writeAutobahnConfig(t, defaultFileConfig(t, []config.AutobahnValidator{validator}))

	app, storage, err := prepareApplication(t.Context(), &config.Config{
		BaseConfig:         config.BaseConfig{Mode: config.ModeValidator},
		AutobahnConfigFile: autobahnConfigFile,
	}, abci.BaseApplication{}, gigaconfig.DefaultConfig, genesisAt(initialHeight))
	require.NoError(t, err)
	manager, ok := storage.Get()
	require.True(t, ok)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })

	_, err = app.InitChain(&abci.RequestInitChain{
		InitialHeight: initialHeight,
		ConsensusParams: &tmproto.ConsensusParams{
			Block: &tmproto.BlockParams{MaxGas: 30_000_000},
		},
	})
	require.NoError(t, err)

	var blockHash [32]byte
	binary.BigEndian.PutUint64(blockHash[:], uint64(initialHeight))
	require.NoError(t, manager.AppHashBuilder().ReportBlockHash(t.Context(), uint64(initialHeight), blockHash))
	_, err = app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Hash: blockHash[:],
		Header: &tmproto.Header{
			Height: initialHeight,
			Time:   time.Unix(1_700_000_000+initialHeight, 0),
		},
	})
	require.NoError(t, err)
	_, err = app.Commit(t.Context())
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		head, err := manager.AppHashVault().GetLatestBlock()
		require.NoError(t, err)
		return head >= uint64(initialHeight)
	}, 10*time.Second, time.Millisecond)
}
