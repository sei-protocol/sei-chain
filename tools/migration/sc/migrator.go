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
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	evidencetypes "github.com/cosmos/cosmos-sdk/x/evidence/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"
	ibchost "github.com/cosmos/ibc-go/v3/modules/core/24-host"
	"github.com/sei-protocol/sei-chain/app/params"
	epochmoduletypes "github.com/sei-protocol/sei-chain/x/epoch/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
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

var Keys = sdk.NewKVStoreKeys(
	acltypes.StoreKey, authtypes.StoreKey, authzkeeper.StoreKey, banktypes.StoreKey, stakingtypes.StoreKey,
	minttypes.StoreKey, distrtypes.StoreKey, slashingtypes.StoreKey,
	govtypes.StoreKey, paramstypes.StoreKey, ibchost.StoreKey, upgradetypes.StoreKey, feegrant.StoreKey,
	evidencetypes.StoreKey, ibctransfertypes.StoreKey, capabilitytypes.StoreKey, oracletypes.StoreKey,
	evmtypes.StoreKey, wasm.StoreKey, epochmoduletypes.StoreKey, tokenfactorytypes.StoreKey,
)

func NewMigrator(homeDir string, db dbm.DB) *Migrator {
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))

	// Creating CMS for store V1
	cmsV1 := rootmulti.NewStore(db, logger)
	for _, key := range Keys {
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
	ssConfig.Enable = false
	cmsV2 := rootmulti2.NewStore(homeDir, logger, scConfig, ssConfig)
	for _, key := range Keys {
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
	next, err := m.storeV2.Restore(uint64(version), types.CurrentFormat, streamReader)
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
	fmt.Printf("Finished writting extension metadata for height: %d\n", height)
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
		keyParams  = sdk.NewKVStoreKey(paramtypes.StoreKey)
		tkeyParams = sdk.NewTransientStoreKey(paramtypes.TStoreKey)
	)
	encodingConfig := params.MakeEncodingConfig()
	pk := paramskeeper.NewKeeper(encodingConfig.Marshaler, encodingConfig.Amino, keyParams, tkeyParams)
	wasmKeeper := keeper.NewKeeper(
		encodingConfig.Marshaler,
		Keys[wasm.StoreKey],
		paramskeeper.Keeper{},
		pk.Subspace("wasm"),
		authkeeper.AccountKeeper{},
		nil,
		stakingkeeper.Keeper{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		filepath.Join(homeDir, "wasm"),
		wasm.DefaultWasmConfig(),
		"iterator,staking,stargate",
	)
	return keeper.NewWasmSnapshotter(cms, &wasmKeeper)

}
