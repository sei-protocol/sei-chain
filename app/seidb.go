package app

import (
	"fmt"

	"github.com/spf13/cast"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/executor/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/storev2/rootmulti"
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
	// Per-block batch size used by the MigrationManager when sc-write-mode
	// is one of the in-flight modes (migrate_evm, migrate_bank,
	// migrate_all_but_bank). Optional: when unset in app.toml the field
	// stays at DefaultStateCommitConfig().KeysToMigratePerBlock (= 1024),
	// which is appropriate for production drains. Lowering it spreads the
	// migration across more blocks, which is useful for tests that need to
	// exercise the resume / hybrid-read path mid-flight.
	FlagSCKeysToMigratePerBlock = "state-commit.sc-keys-to-migrate-per-block"

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
		panic("SeiDB state-commit (SC) must be enabled; IAVL backend has been fully deprecated")
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
<<<<<<< HEAD
		parsedWM, err := config.ParseWriteMode(wm)
=======
		parsedWM, err := config.ParseSCWriteMode(wm)
>>>>>>> e4257d5 (Accept legacy cosmos_only SC write mode (#3704))
		if err != nil {
			panic(fmt.Sprintf("invalid SC write mode %q: %s", wm, err))
		}
		scConfig.WriteMode = parsedWM
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
