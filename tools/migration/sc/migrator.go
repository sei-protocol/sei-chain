package sc

import (
	"fmt"
	"io"
	"os"

	"github.com/CosmWasm/wasmd/x/wasm"
	"github.com/cosmos/cosmos-sdk/snapshots"
	"github.com/cosmos/cosmos-sdk/snapshots/types"
	"github.com/cosmos/cosmos-sdk/store"
	"github.com/cosmos/cosmos-sdk/store/rootmulti"
	rootmulti2 "github.com/cosmos/cosmos-sdk/storev2/rootmulti"
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
	"github.com/sei-protocol/sei-db/config"
	"github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"
)

type Migrator struct {
	logger  log.Logger
	storeV1 store.CommitMultiStore
	storeV2 store.CommitMultiStore
}

func NewMigrator(homeDir string, db dbm.DB) *Migrator {
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))
	keys := sdk.NewKVStoreKeys(
		acltypes.StoreKey, authtypes.StoreKey, authzkeeper.StoreKey, banktypes.StoreKey, stakingtypes.StoreKey,
		minttypes.StoreKey, distrtypes.StoreKey, slashingtypes.StoreKey,
		govtypes.StoreKey, paramstypes.StoreKey, ibchost.StoreKey, upgradetypes.StoreKey, feegrant.StoreKey,
		evidencetypes.StoreKey, ibctransfertypes.StoreKey, capabilitytypes.StoreKey, oracletypes.StoreKey,
		evmtypes.StoreKey, wasm.StoreKey, epochmoduletypes.StoreKey, tokenfactorytypes.StoreKey,
	)
	// Creating CMS for store V1
	cmsV1 := rootmulti.NewStore(db, logger)
	for _, key := range keys {
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
	for _, key := range keys {
		cmsV2.MountStoreWithDB(key, sdk.StoreTypeIAVL, db)
	}
	err = cmsV2.LoadLatestVersion()
	if err != nil {
		panic(err)
	}
	return &Migrator{
		logger:  logger,
		storeV1: cmsV1,
		storeV2: cmsV2,
	}
}

func (m *Migrator) Migrate(version int64) error {
	// Create a snapshot
	chunks := make(chan io.ReadCloser)
	go m.createSnapshot(uint64(version), chunks)
	streamReader, err := snapshots.NewStreamReader(chunks)
	if err != nil {
		return err
	}
	fmt.Printf("Start restoring SC store for height: %d\n", version)
	nextItem, err := m.storeV2.Restore(uint64(version), types.CurrentFormat, streamReader)
	for {
		if nextItem.Item == nil {
			// end of stream
			break
		}
		// TODO add extension
	}
	fmt.Printf("Finished restoring SC store for height: %d\n", version)
	return nil
}

func (m *Migrator) createSnapshot(height uint64, chunks chan<- io.ReadCloser) {
	streamWriter := snapshots.NewStreamWriter(chunks)
	defer streamWriter.Close()
	fmt.Printf("Start creating snapshot for height: %d\n", height)
	if err := m.storeV1.Snapshot(height, streamWriter); err != nil {
		m.logger.Error("Snapshot creation failed", "err", err)
		streamWriter.CloseWithError(err)
	}
	// TODO: add extension
}
