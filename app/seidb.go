package app

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/storev2/rootmulti"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	seidb "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/spf13/cast"
	"github.com/tendermint/tendermint/libs/log"
)

const (
	// SC Store configs
	FlagSCEnable                     = "state-commit.sc-enable"
	FlagSCDirectory                  = "state-commit.sc-directory"
	FlagSCAsyncCommitBuffer          = "state-commit.sc-async-commit-buffer"
	FlagSCSnapshotKeepRecent         = "state-commit.sc-keep-recent"
	FlagSCSnapshotInterval           = "state-commit.sc-snapshot-interval"
	FlagSCSnapshotMinTimeInterval    = "state-commit.sc-snapshot-min-time-interval"
	FlagSCSnapshotWriterLimit        = "state-commit.sc-snapshot-writer-limit"
	FlagSCSnapshotPrefetchThreshold  = "state-commit.sc-snapshot-prefetch-threshold"
	FlagSCSnapshotWriteRateMBps      = "state-commit.sc-snapshot-write-rate-mbps"
	FlagSCHistoricalProofMaxInFlight = "state-commit.sc-historical-proof-max-inflight"
	FlagSCHistoricalProofRateLimit   = "state-commit.sc-historical-proof-rate-limit"
	FlagSCHistoricalProofBurst       = "state-commit.sc-historical-proof-burst"
	FlagSCWriteMode                  = "state-commit.sc-write-mode"
	FlagSCReadMode                   = "state-commit.sc-read-mode"

	// SS Store configs
	FlagSSEnable            = "state-store.ss-enable"
	FlagSSDirectory         = "state-store.ss-db-directory"
	FlagSSBackend           = "state-store.ss-backend"
	FlagSSAsyncWriterBuffer = "state-store.ss-async-write-buffer"
	FlagSSKeepRecent        = "state-store.ss-keep-recent"
	FlagSSPruneInterval     = "state-store.ss-prune-interval"
	FlagSSImportNumWorkers  = "state-store.ss-import-num-workers"

	// EVM SS optimization (embedded in SS config, controlled via write/read mode)
	FlagEVMSSDirectory = "state-store.evm-ss-db-directory"
	FlagEVMSSWriteMode = "state-store.evm-ss-write-mode"
	FlagEVMSSReadMode  = "state-store.evm-ss-read-mode"

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
	if ssConfig.EVMEnabled() {
		logger.Info("SeiDB EVM StateStore optimization is enabled",
			"writeMode", ssConfig.WriteMode, "readMode", ssConfig.ReadMode)
	}
	validateConfigs(appOpts)

	// cms must be overridden before the other options, because they may use the cms,
	// make sure the cms aren't be overridden by the other options later on.
	cms := rootmulti.NewStore(homePath, logger, scConfig, ssConfig, cast.ToBool(appOpts.Get("migrate-iavl")))
	migrationEnabled := cast.ToBool(appOpts.Get(FlagMigrateIAVL))
	migrationHeight := cast.ToInt64(appOpts.Get(FlagMigrateHeight))
	baseAppOptions = append([]func(*baseapp.BaseApp){
		func(baseApp *baseapp.BaseApp) {
			if migrationEnabled || migrationHeight > 0 {
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
	scConfig.MemIAVLConfig.AsyncCommitBuffer = cast.ToInt(appOpts.Get(FlagSCAsyncCommitBuffer))
	scConfig.MemIAVLConfig.SnapshotKeepRecent = cast.ToUint32(appOpts.Get(FlagSCSnapshotKeepRecent))
	scConfig.MemIAVLConfig.SnapshotInterval = cast.ToUint32(appOpts.Get(FlagSCSnapshotInterval))
	scConfig.MemIAVLConfig.SnapshotMinTimeInterval = cast.ToUint32(appOpts.Get(FlagSCSnapshotMinTimeInterval))
	scConfig.MemIAVLConfig.SnapshotWriterLimit = cast.ToInt(appOpts.Get(FlagSCSnapshotWriterLimit))
	scConfig.MemIAVLConfig.SnapshotPrefetchThreshold = cast.ToFloat64(appOpts.Get(FlagSCSnapshotPrefetchThreshold))
	scConfig.MemIAVLConfig.SnapshotWriteRateMBps = cast.ToInt(appOpts.Get(FlagSCSnapshotWriteRateMBps))
	scConfig.HistoricalProofMaxInFlight = cast.ToInt(appOpts.Get(FlagSCHistoricalProofMaxInFlight))
	scConfig.HistoricalProofRateLimit = cast.ToFloat64(appOpts.Get(FlagSCHistoricalProofRateLimit))
	scConfig.WriteMode = config.WriteMode(cast.ToString(appOpts.Get(FlagSCWriteMode)))
	scConfig.ReadMode = config.ReadMode(cast.ToString(appOpts.Get(FlagSCReadMode)))

	if v := appOpts.Get(FlagSCHistoricalProofBurst); v != nil {
		scConfig.HistoricalProofBurst = cast.ToInt(v)
	}
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

	// EVM optimization fields (embedded in SS config)
	ssConfig.EVMDBDirectory = cast.ToString(appOpts.Get(FlagEVMSSDirectory))
	if wm := cast.ToString(appOpts.Get(FlagEVMSSWriteMode)); wm != "" {
		parsedWM, err := config.ParseWriteMode(wm)
		if err != nil {
			panic(fmt.Sprintf("invalid EVM SS write mode %q: %s", wm, err))
		}
		ssConfig.WriteMode = parsedWM
	}
	if rm := cast.ToString(appOpts.Get(FlagEVMSSReadMode)); rm != "" {
		parsedRM, err := config.ParseReadMode(rm)
		if err != nil {
			panic(fmt.Sprintf("invalid EVM SS read mode %q: %s", rm, err))
		}
		ssConfig.ReadMode = parsedRM
	}
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
