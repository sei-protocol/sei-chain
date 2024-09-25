package app

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/storev2/rootmulti"
	"github.com/sei-protocol/sei-db/config"
	seidb "github.com/sei-protocol/sei-db/ss/types"
	"github.com/spf13/cast"
	"github.com/tendermint/tendermint/libs/log"
)

const (
	// SC Store configs
	FlagSCEnable              = "state-commit.sc-enable"
	FlagSCDirectory           = "state-commit.sc-directory"
	FlagSCAsyncCommitBuffer   = "state-commit.sc-async-commit-buffer"
	FlagSCZeroCopy            = "state-commit.sc-zero-copy"
	FlagSCSnapshotKeepRecent  = "state-commit.sc-keep-recent"
	FlagSCSnapshotInterval    = "state-commit.sc-snapshot-interval"
	FlagSCSnapshotWriterLimit = "state-commit.sc-snapshot-writer-limit"
	FlagSCCacheSize           = "state-commit.sc-cache-size"

	// SS Store configs
	FlagSSEnable            = "state-store.ss-enable"
	FlagSSDirectory         = "state-store.ss-db-directory"
	FlagSSBackend           = "state-store.ss-backend"
	FlagSSAsyncWriterBuffer = "state-store.ss-async-write-buffer"
	FlagSSKeepRecent        = "state-store.ss-keep-recent"
	FlagSSPruneInterval     = "state-store.ss-prune-interval"
	FlagSSImportNumWorkers  = "state-store.ss-import-num-workers"

	// Other configs
	FlagSnapshotInterval = "state-sync.snapshot-interval"
	FlagMigrateIAVL      = "migrate-iavl"
	FlagMigrateHeight    = "migrate-height"
)

func SetupSeiDB(
	logger log.Logger,
	homePath string,
	appOpts servertypes.AppOptions,
	baseAppOptions []func(*baseapp.BaseApp),
) ([]func(*baseapp.BaseApp), seidb.StateStore) {
	scEnabled := cast.ToBool(appOpts.Get(FlagSCEnable))
	if !scEnabled {
		logger.Info("SeiDB is disabled, falling back to IAVL")
		return baseAppOptions, nil
	}
	logger.Info("SeiDB SC is enabled, running node with StoreV2 commit store")
	scConfig := parseSCConfigs(appOpts)
	ssConfig := parseSSConfigs(appOpts)
	if ssConfig.Enable {
		logger.Info(fmt.Sprintf("SeiDB StateStore is enabled, running %s for historical state", ssConfig.Backend))
	}
	validateConfigs(appOpts)

	// cms must be overridden before the other options, because they may use the cms,
	// make sure the cms aren't be overridden by the other options later on.
	cms := rootmulti.NewStore(homePath, logger, scConfig, ssConfig)
	migrationEnabled := cast.ToBool(appOpts.Get(FlagMigrateIAVL))
	migrationHeight := cast.ToInt64(appOpts.Get(FlagMigrateHeight))
	baseAppOptions = append([]func(*baseapp.BaseApp){
		func(baseApp *baseapp.BaseApp) {
			if migrationEnabled {
				originalCMS := baseApp.CommitMultiStore()
				baseApp.SetQueryMultiStore(originalCMS)
				baseApp.SetMigrationHeight(migrationHeight)
			}
			baseApp.SetCMS(cms)
		},
	}, baseAppOptions...)

	return baseAppOptions, cms.GetStateStore()
}

func parseSCConfigs(appOpts servertypes.AppOptions) config.StateCommitConfig {
	scConfig := config.DefaultStateCommitConfig()
	scConfig.Enable = cast.ToBool(appOpts.Get(FlagSCEnable))
	scConfig.Directory = cast.ToString(appOpts.Get(FlagSCDirectory))
	scConfig.ZeroCopy = cast.ToBool(appOpts.Get(FlagSCZeroCopy))
	scConfig.AsyncCommitBuffer = cast.ToInt(appOpts.Get(FlagSCAsyncCommitBuffer))
	scConfig.SnapshotKeepRecent = cast.ToUint32(appOpts.Get(FlagSCSnapshotKeepRecent))
	scConfig.SnapshotInterval = cast.ToUint32(appOpts.Get(FlagSCSnapshotInterval))
	scConfig.SnapshotWriterLimit = cast.ToInt(appOpts.Get(FlagSCSnapshotWriterLimit))
	return scConfig
}

func parseSSConfigs(appOpts servertypes.AppOptions) config.StateStoreConfig {
	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Enable = cast.ToBool(appOpts.Get(FlagSSEnable))
	ssConfig.Backend = cast.ToString(appOpts.Get(FlagSSBackend))
	ssConfig.AsyncWriteBuffer = cast.ToInt(appOpts.Get(FlagSSAsyncWriterBuffer))
	ssConfig.KeepRecent = cast.ToInt(appOpts.Get(FlagSSKeepRecent))
	ssConfig.PruneIntervalSeconds = cast.ToInt(appOpts.Get(FlagSSPruneInterval))
	ssConfig.ImportNumWorkers = cast.ToInt(appOpts.Get(FlagSSImportNumWorkers))
	ssConfig.DBDirectory = cast.ToString(appOpts.Get(FlagSSDirectory))
	return ssConfig
}

func validateConfigs(appOpts servertypes.AppOptions) {
	scEnabled := cast.ToBool(appOpts.Get(FlagSCEnable))
	ssEnabled := cast.ToBool(appOpts.Get(FlagSSEnable))
	snapshotExportInterval := cast.ToUint64(appOpts.Get(FlagSnapshotInterval))
	// Make sure when snapshot is enabled, we should enable SS store
	if snapshotExportInterval > 0 && scEnabled {
		if !ssEnabled {
			panic(fmt.Sprintf("Config validation failed, SeiDB SS store needs to be enabled when snapshot interval %d > 0", snapshotExportInterval))
		}
	}
}
