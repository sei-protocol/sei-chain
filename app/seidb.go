package app

import (
	"fmt"

	"github.com/spf13/cast"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/executor/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/storev2/rootmulti"
	"github.com/sei-protocol/sei-chain/sei-cosmos/version"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	seidb "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
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
	FlagSCFlatKVSnapshotInterval     = "state-commit.flatkv.snapshot-interval"
	FlagSCFlatKVSnapshotKeepRecent   = "state-commit.flatkv.snapshot-keep-recent"
	// Per-block batch size used by the MigrationManager when sc-write-mode
	// is one of the in-flight modes (migrate_evm, migrate_bank,
	// migrate_all_but_bank). Optional: when unset in app.toml the field
	// stays at DefaultStateCommitConfig().KeysToMigratePerBlock (= 1024),
	// which is appropriate for production drains. Lowering it spreads the
	// migration across more blocks, which is useful for tests that need to
	// exercise the resume / hybrid-read path mid-flight.
	FlagSCKeysToMigratePerBlock = "state-commit.sc-keys-to-migrate-per-block"

	// Block height at which the EVM migration begins when sc-write-mode is
	// migrate_evm. When unset or 0 the migration starts immediately, matching
	// the historical behavior. When > 0 the node runs memiavl-only-equivalent
	// for the EVM module until this height, then begins draining.
	FlagSCMigrateEVMStartHeight = "state-commit.sc-migrate-evm-start-height"

	// Hash logger configs (per-block hash logging; enabled by default)
	FlagSCHashLoggerEnable         = "state-commit.sc-hash-logger-enable"
	FlagSCHashLoggerDirectory      = "state-commit.sc-hash-logger-directory"
	FlagSCHashLoggerBlocksToRetain = "state-commit.sc-hash-logger-blocks-to-retain"
	FlagSCHashLoggerTargetFileSize = "state-commit.sc-hash-logger-target-file-size"
	FlagSCHashLoggerMaxDiskSize    = "state-commit.sc-hash-logger-max-disk-size"

	// SS Store configs
	FlagSSEnable            = "state-store.ss-enable"
	FlagSSDirectory         = "state-store.ss-db-directory"
	FlagSSBackend           = "state-store.ss-backend"
	FlagSSAsyncWriterBuffer = "state-store.ss-async-write-buffer"
	FlagSSKeepRecent        = "state-store.ss-keep-recent"
	FlagSSPruneInterval     = "state-store.ss-prune-interval"
	FlagSSImportNumWorkers  = "state-store.ss-import-num-workers"

	// EVM SS optimization (embedded in SS config, controlled via write/read mode)
	FlagEVMSSDirectory   = "state-store.evm-ss-db-directory"
	FlagEVMSSSplit       = "state-store.evm-ss-split"
	FlagEVMSSSeparateDBs = "state-store.evm-ss-separate-dbs"

	// Other configs
	FlagSnapshotInterval = "state-sync.snapshot-interval"
)

var GigaKeys = []string{"evm", "bank"}

func SetupSeiDB(
	homePath string,
	appOpts servertypes.AppOptions,
	baseAppOptions []func(*baseapp.BaseApp),
) ([]func(*baseapp.BaseApp), seidb.StateStore) {
	scEnabled := cast.ToBool(appOpts.Get(FlagSCEnable))
	if !scEnabled {
		logger.Warn("IAVL will be deprecated soon, please migrate to SeiDB to avoid data corruption or panic")
		return baseAppOptions, nil
	}
	scConfig := parseSCConfigs(appOpts)
	logger.Info("SeiDB SC is enabled now", "sc-config", scConfig)
	ssConfig := parseSSConfigs(appOpts)
	if ssConfig.Enable {
		logger.Info("SeiDB SS is enabled", "backend", ssConfig.Backend)
	}
	if ssConfig.EVMSplit {
		logger.Info("SeiDB EVM StateStore optimization is enabled",
			"separateDBs", ssConfig.SeparateEVMSubDBs,
		)
	}
	validateConfigs(appOpts)
	gigaExecutorConfig, err := gigaconfig.ReadConfig(appOpts)
	if err != nil {
		panic(fmt.Sprintf("error reading giga executor config due to %s", err))
	}
	gigaStoreKeys := []string{}
	if gigaExecutorConfig.Enabled {
		gigaStoreKeys = GigaKeys
	}
	// cms must be overridden before the other options, because they may use the cms,
	// make sure the cms aren't be overridden by the other options later on.
	cms := rootmulti.NewStore(homePath, scConfig, ssConfig, gigaStoreKeys)
	baseAppOptions = append([]func(*baseapp.BaseApp){
		func(baseApp *baseapp.BaseApp) {
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

	if wm := cast.ToString(appOpts.Get(FlagSCWriteMode)); wm != "" {
		parsedWM, err := config.ParseWriteMode(wm)
		if err != nil {
			panic(fmt.Sprintf("invalid EVM SS write mode %q: %s", wm, err))
		}
		scConfig.WriteMode = parsedWM
	}
	if v := appOpts.Get(FlagSCFlatKVSnapshotInterval); v != nil {
		scConfig.FlatKVConfig.SnapshotInterval = cast.ToUint32(v)
	}
	if v := appOpts.Get(FlagSCFlatKVSnapshotKeepRecent); v != nil {
		scConfig.FlatKVConfig.SnapshotKeepRecent = cast.ToUint32(v)
	}

	if v := appOpts.Get(FlagSCHistoricalProofMaxInFlight); v != nil {
		scConfig.HistoricalProofMaxInFlight = cast.ToInt(v)
	}
	if v := appOpts.Get(FlagSCHistoricalProofRateLimit); v != nil {
		scConfig.HistoricalProofRateLimit = cast.ToFloat64(v)
	}
	if v := appOpts.Get(FlagSCHistoricalProofBurst); v != nil {
		scConfig.HistoricalProofBurst = cast.ToInt(v)
	}
	// Guard with v != nil so that an absent app.toml entry preserves the
	// default of 1024 instead of clobbering it to 0, which would fail
	// StateCommitConfig.Validate ("keys-to-migrate-per-block must be > 0")
	// and bring the node down at startup the first time write-mode is
	// flipped to a migration mode.
	if v := appOpts.Get(FlagSCKeysToMigratePerBlock); v != nil {
		if n := cast.ToInt(v); n > 0 {
			scConfig.KeysToMigratePerBlock = n
		}
	}
	// Optional: defer the start of the EVM migration to a fixed height. Absent
	// or 0 preserves the historical "start immediately" behavior.
	if v := appOpts.Get(FlagSCMigrateEVMStartHeight); v != nil {
		if h := cast.ToInt64(v); h > 0 {
			scConfig.MigrateEVMStartHeight = h
		}
	}

	// Hash logger. Guard each read with v != nil so an absent app.toml entry preserves the default
	// (notably Enable, which defaults to true) instead of clobbering it to the zero value.
	if v := appOpts.Get(FlagSCHashLoggerEnable); v != nil {
		scConfig.HashLogger.Enable = cast.ToBool(v)
	}
	if v := appOpts.Get(FlagSCHashLoggerDirectory); v != nil {
		scConfig.HashLogger.Directory = cast.ToString(v)
	}
	// BlocksToRetain and MaxDiskSize take a configured value verbatim, including 0 (which disables that
	// retention dimension). TargetFileSize must stay > 0, so a 0/absent value preserves the default.
	if v := appOpts.Get(FlagSCHashLoggerBlocksToRetain); v != nil {
		scConfig.HashLogger.BlocksToRetain = cast.ToUint(v)
	}
	if v := appOpts.Get(FlagSCHashLoggerTargetFileSize); v != nil {
		if n := cast.ToUint(v); n > 0 {
			scConfig.HashLogger.TargetFileSize = n
		}
	}
	if v := appOpts.Get(FlagSCHashLoggerMaxDiskSize); v != nil {
		scConfig.HashLogger.MaxDiskSize = cast.ToUint(v)
	}
	// The software version is embedded in hash log file names so archives from different builds are
	// distinguishable. Sourced from the node build version, not from app.toml.
	scConfig.HashLogger.Version = version.Version

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
	ssConfig.SeparateEVMSubDBs = cast.ToBool(appOpts.Get(FlagEVMSSSeparateDBs))
	ssConfig.EVMSplit = cast.ToBool(appOpts.Get(FlagEVMSSSplit))
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
