package sc

import (
	"fmt"
	"github.com/cosmos/cosmos-sdk/store"
	rootmulti2 "github.com/cosmos/cosmos-sdk/storev2/rootmulti"
	"github.com/sei-protocol/sei-db/config"
	"path/filepath"

	"github.com/CosmWasm/wasmd/x/wasm"
	"github.com/cosmos/cosmos-sdk/snapshots"
	"github.com/cosmos/cosmos-sdk/store/rootmulti"
	sdk "github.com/cosmos/cosmos-sdk/types"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	evidencetypes "github.com/cosmos/cosmos-sdk/x/evidence/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"
	ibchost "github.com/cosmos/ibc-go/v3/modules/core/24-host"
	epochmoduletypes "github.com/sei-protocol/sei-chain/x/epoch/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
	"github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"
)

type Migrator struct {
	storeV1 store.CommitMultiStore
	storeV2 store.CommitMultiStore
}

func NewMigrator(homeDir string, db dbm.DB) *Migrator {
	keys := sdk.NewKVStoreKeys(
		acltypes.StoreKey, authtypes.StoreKey, authzkeeper.StoreKey, banktypes.StoreKey, stakingtypes.StoreKey,
		minttypes.StoreKey, distrtypes.StoreKey, slashingtypes.StoreKey,
		govtypes.StoreKey, paramstypes.StoreKey, ibchost.StoreKey, upgradetypes.StoreKey, feegrant.StoreKey,
		evidencetypes.StoreKey, ibctransfertypes.StoreKey, capabilitytypes.StoreKey, oracletypes.StoreKey,
		evmtypes.StoreKey, wasm.StoreKey, epochmoduletypes.StoreKey, tokenfactorytypes.StoreKey,
	)
	// Creating CMS for store V1
	cmsV1 := rootmulti.NewStore(db, log.NewNopLogger())
	for _, key := range keys {
		cmsV1.MountStoreWithDB(key, sdk.StoreTypeIAVL, db)
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
	cmsV2 := rootmulti2.NewStore(homeDir, log.NewNopLogger(), scConfig, ssConfig)
	for _, key := range keys {
		cmsV2.MountStoreWithDB(key, sdk.StoreTypeIAVL, db)
	}
	err = cmsV2.LoadLatestVersion()
	if err != nil {
		panic(err)
	}
	return &Migrator{
		storeV1: cmsV1,
		storeV2: cmsV2,
	}
}

func (m *Migrator) Migrate(version int64, homeDir string) error {
	// Create a snapshot
	dataDir := filepath.Join(homeDir, "data")
	snapshotDirectory := filepath.Join(dataDir, "snapshots")
	snapshotDB, err := sdk.NewLevelDB("metadata", snapshotDirectory)
	if err != nil {
		panic(err)
	}
	snapshotStore, err := snapshots.NewStore(snapshotDB, snapshotDirectory)
	manager := snapshots.NewManager(snapshotStore, m.storeV1, log.NewNopLogger())
	fmt.Printf("Start creating snapshot in %s for height %d\n", snapshotDirectory, version)
	snapshot, err := manager.CreateAndMaybeWait(uint64(version), true)
	if err != nil {
		return fmt.Errorf("failed to create state snapshot")
	}
	fmt.Printf("Finished creating snapshot in %s for height %d\n", snapshotDirectory, snapshot.Height)
	manager.Close()

	// Restore from a snapshot
	manager = snapshots.NewManager(snapshotStore, m.storeV2, log.NewNopLogger())
	fmt.Printf("Start restoring from snapshot height %d\n", snapshot.Height)
	err = manager.Restore(*snapshot)
	if err != nil {
		return err
	}
	fmt.Printf("Finished restoring SC store from snapshot height %d\n", snapshot.Height)
	return nil
}
