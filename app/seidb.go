package app

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/storev2/rootmulti"
	"github.com/sei-protocol/sei-db/config"
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
)

func SetupSeiDB(
	logger log.Logger,
	homePath string,
	appOpts servertypes.AppOptions,
	baseAppOptions []func(*baseapp.BaseApp),
) []func(*baseapp.BaseApp) {
	scEnabled := cast.ToBool(appOpts.Get(FlagSCEnable))
	if !scEnabled {
		logger.Info("SeiDB is disabled, falling back to IAVL")
		return baseAppOptions
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
	baseAppOptions = append([]func(*baseapp.BaseApp){
		func(baseApp *baseapp.BaseApp) {
			baseApp.SetCMS(cms)
		},
	}, baseAppOptions...)

	return baseAppOptions
}

func parseSCConfigs(appOpts servertypes.AppOptions) config.StateCommitConfig {
	return config.StateCommitConfig{
		Enable:              cast.ToBool(appOpts.Get(FlagSCEnable)),
		Directory:           cast.ToString(appOpts.Get(FlagSCDirectory)),
		ZeroCopy:            cast.ToBool(appOpts.Get(FlagSCZeroCopy)),
		AsyncCommitBuffer:   cast.ToInt(appOpts.Get(FlagSCAsyncCommitBuffer)),
		SnapshotKeepRecent:  cast.ToUint32(appOpts.Get(FlagSCSnapshotKeepRecent)),
		SnapshotInterval:    cast.ToUint32(appOpts.Get(FlagSCSnapshotInterval)),
		SnapshotWriterLimit: cast.ToInt(appOpts.Get(FlagSCSnapshotWriterLimit)),
		CacheSize:           cast.ToInt(appOpts.Get(FlagSCCacheSize)),
	}
}

func parseSSConfigs(appOpts servertypes.AppOptions) config.StateStoreConfig {
	return config.StateStoreConfig{
		Enable:               cast.ToBool(appOpts.Get(FlagSSEnable)),
		Backend:              cast.ToString(appOpts.Get(FlagSSBackend)),
		AsyncWriteBuffer:     cast.ToInt(appOpts.Get(FlagSSAsyncWriterBuffer)),
		KeepRecent:           cast.ToInt(appOpts.Get(FlagSSKeepRecent)),
		PruneIntervalSeconds: cast.ToInt(appOpts.Get(FlagSSPruneInterval)),
		ImportNumWorkers:     cast.ToInt(appOpts.Get(FlagSSImportNumWorkers)),
		DBDirectory:          cast.ToString(appOpts.Get(FlagSSDirectory)),
	}
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
