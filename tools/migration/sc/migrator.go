package sc

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/CosmWasm/wasmd/x/wasm"
	"github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/cosmos/cosmos-sdk/snapshots"
	"github.com/cosmos/cosmos-sdk/snapshots/types"
	"github.com/cosmos/cosmos-sdk/store"
	"github.com/cosmos/cosmos-sdk/store/rootmulti"
	rootmulti2 "github.com/cosmos/cosmos-sdk/storev2/rootmulti"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"
	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/tools/utils"
	"github.com/sei-protocol/sei-db/config"
	"github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"
)

type Migrator struct {
	homeDir string
	logger  log.Logger
	storeV1 store.CommitMultiStore
	storeV2 store.CommitMultiStore
}

func NewMigrator(homeDir string, db dbm.DB) *Migrator {
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))

	// Creating CMS for store V1
	cmsV1 := rootmulti.NewStore(db, logger)
	for _, key := range utils.ModuleKeys {
		cmsV1.MountStoreWithDB(key, sdk.StoreTypeIAVL, nil)
	}
	err := cmsV1.LoadLatestVersion()
	if err != nil {
		panic(err)
	}

	// Creating CMS for store V2
	scConfig := config.DefaultStateCommitConfig()
	scConfig.Enable = true
	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Enable = true
	ssConfig.KeepRecent = 0
	cmsV2 := rootmulti2.NewStore(homeDir, logger, scConfig, ssConfig, true)
	for _, key := range utils.ModuleKeys {
		cmsV2.MountStoreWithDB(key, sdk.StoreTypeIAVL, db)
	}
	err = cmsV2.LoadLatestVersion()
	if err != nil {
		panic(err)
	}
	return &Migrator{
		homeDir: homeDir,
		logger:  logger,
		storeV1: cmsV1,
		storeV2: cmsV2,
	}
}

func (m *Migrator) Migrate(version int64) error {
	// Create a snapshot
	chunks := make(chan io.ReadCloser)
	go func() {
		err := m.createSnapshot(uint64(version), chunks)
		if err != nil {
			panic(err)
		}
	}()
	streamReader, err := snapshots.NewStreamReader(chunks)
	if err != nil {
		return err
	}
	fmt.Printf("Start restoring SC store for height: %d\n", version)
	next, _ := m.storeV2.Restore(uint64(version), types.CurrentFormat, streamReader)
	for {
		if next.Item == nil {
			// end of stream
			break
		}
		metadata := next.GetExtension()
		if metadata == nil {
			return sdkerrors.Wrapf(sdkerrors.ErrLogic, "unknown snapshot item %T", next.Item)
		}
		wasmSnapshotter := CreateWasmSnapshotter(m.storeV2, m.homeDir)
		extension := wasmSnapshotter
		fmt.Printf("Start restoring wasm extension for height: %d\n", version)
		next, err = extension.Restore(uint64(version), metadata.Format, streamReader)
		if err != nil {
			return sdkerrors.Wrapf(err, "extension %s restore", metadata.Name)
		}
	}
	fmt.Printf("Finished restoring SC store for height: %d\n", version)
	return nil
}

func (m *Migrator) createSnapshot(height uint64, chunks chan<- io.ReadCloser) error {
	streamWriter := snapshots.NewStreamWriter(chunks)
	defer streamWriter.Close()
	fmt.Printf("Start creating snapshot for height: %d\n", height)
	if err := m.storeV1.Snapshot(height, streamWriter); err != nil {
		m.logger.Error("Snapshot creation failed", "err", err)
		streamWriter.CloseWithError(err)
	}

	// Handle wasm snapshot export
	wasmSnapshotter := CreateWasmSnapshotter(m.storeV1, m.homeDir)
	extension := wasmSnapshotter
	// write extension metadata
	err := streamWriter.WriteMsg(&types.SnapshotItem{
		Item: &types.SnapshotItem_Extension{
			Extension: &types.SnapshotExtensionMeta{
				Name:   wasmSnapshotter.SnapshotName(),
				Format: extension.SnapshotFormat(),
			},
		},
	})
	fmt.Printf("Finished writing extension metadata for height: %d\n", height)
	if err != nil {
		streamWriter.CloseWithError(err)
		return err
	}
	fmt.Printf("Start extension snapshot for height: %d\n", height)
	if err = extension.Snapshot(height, streamWriter); err != nil {
		streamWriter.CloseWithError(err)
		return err
	}
	return nil

}

func CreateWasmSnapshotter(cms sdk.MultiStore, homeDir string) *keeper.WasmSnapshotter {
	var (
		keyParams  = sdk.NewKVStoreKey(paramstypes.StoreKey)
		tkeyParams = sdk.NewTransientStoreKey(paramstypes.TStoreKey)
	)
	encodingConfig := params.MakeEncodingConfig()
	pk := paramskeeper.NewKeeper(encodingConfig.Marshaler, encodingConfig.Amino, keyParams, tkeyParams)
	wasmKeeper := keeper.NewKeeper(
		encodingConfig.Marshaler,
		utils.ModuleKeys[wasm.StoreKey],
		paramskeeper.Keeper{},
		pk.Subspace("wasm"),
		authkeeper.AccountKeeper{},
		nil,
		stakingkeeper.Keeper{},
		nil,
		nil,
		nil,
		nil,
		upgradekeeper.Keeper{},
		nil,
		nil,
		nil,
		filepath.Join(homeDir, "wasm"),
		wasm.DefaultWasmConfig(),
		"iterator,staking,stargate,sei",
	)
	return keeper.NewWasmSnapshotter(cms, &wasmKeeper)

}
