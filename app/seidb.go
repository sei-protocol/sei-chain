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
	FlagSCWriteModeEnableAuto        = "state-commit.sc-write-mode-enable-auto"
	FlagSCFlatKVReadWriteMetrics     = "state-commit.flatkv.enable-read-write-metrics"

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
	FlagSSReadWriteMetrics  = "state-store.ss-enable-read-write-metrics"

	// EVM SS optimization (embedded in SS config, controlled via write/read mode)
	FlagEVMSSDirectory   = "state-store.evm-ss-db-directory"
	FlagEVMSSSplit       = "state-store.evm-ss-split"
	FlagEVMSSSeparateDBs = "state-store.evm-ss-separate-dbs"
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
	// Guard every read with a presence check: an absent app.toml key must
	// preserve the in-code default from DefaultStateCommitConfig above rather
	// than reading back the zero value (cast.To*(nil) == 0/false) and clobbering
	// it. This matters for keys whose default is non-zero (async-commit-buffer
	// 100, snapshot-interval 10000, keep-recent 1, ...) so a config that omits a
	// key does not silently downgrade the node (e.g. to synchronous commits).
	if v := appOpts.Get(FlagSCAsyncCommitBuffer); v != nil {
		scConfig.MemIAVLConfig.AsyncCommitBuffer = cast.ToInt(v)
	}
	if v := appOpts.Get(FlagSCSnapshotKeepRecent); v != nil {
		scConfig.MemIAVLConfig.SnapshotKeepRecent = cast.ToUint32(v)
	}
	if v := appOpts.Get(FlagSCSnapshotInterval); v != nil {
		scConfig.MemIAVLConfig.SnapshotInterval = cast.ToUint32(v)
	}
	if v := appOpts.Get(FlagSCSnapshotMinTimeInterval); v != nil {
		scConfig.MemIAVLConfig.SnapshotMinTimeInterval = cast.ToUint32(v)
	}
	if v := appOpts.Get(FlagSCSnapshotWriterLimit); v != nil {
		scConfig.MemIAVLConfig.SnapshotWriterLimit = cast.ToInt(v)
	}
	if v := appOpts.Get(FlagSCSnapshotPrefetchThreshold); v != nil {
		scConfig.MemIAVLConfig.SnapshotPrefetchThreshold = cast.ToFloat64(v)
	}
	if v := appOpts.Get(FlagSCSnapshotWriteRateMBps); v != nil {
		scConfig.MemIAVLConfig.SnapshotWriteRateMBps = cast.ToInt(v)
	}
	if v := appOpts.Get(FlagSCFlatKVReadWriteMetrics); v != nil {
		scConfig.FlatKVConfig.EnableReadWriteMetrics = cast.ToBool(v)
	}

	// sc-write-mode-enable-auto (default true) decides whether the node may run
	// in auto. An ABSENT key keeps the default (true): nodes provisioned by
	// older binaries carry an explicit sc-write-mode = "memiavl_only" but no
	// sc-write-mode-enable-auto key, and must still resolve to auto so a
	// governance-driven migration can start without an app.toml edit. Only an
	// explicit key flips it.
	if v := appOpts.Get(FlagSCWriteModeEnableAuto); v != nil {
		scConfig.WriteModeEnableAuto = cast.ToBool(v)
	}
	// Always parse sc-write-mode (even when auto is on) so a typo'd value fails
	// fast here exactly as it does in server/config.GetConfig.
	if wm := cast.ToString(appOpts.Get(FlagSCWriteMode)); wm != "" {
		parsedWM, err := config.ParseSCWriteMode(wm)
		if err != nil {
			panic(fmt.Sprintf("invalid SC write mode %q: %s", wm, err))
		}
		scConfig.WriteMode = parsedWM
	}
	// When auto is enabled the explicit sc-write-mode is ignored and the node
	// runs in auto; only when auto is disabled is the parsed mode honored (see
	// config.ApplyWriteModeAuto).
	scConfig.WriteMode = config.ApplyWriteModeAuto(scConfig.WriteModeEnableAuto, scConfig.WriteMode)

	if v := appOpts.Get(FlagSCHistoricalProofMaxInFlight); v != nil {
		scConfig.HistoricalProofMaxInFlight = cast.ToInt(v)
	}
	if v := appOpts.Get(FlagSCHistoricalProofRateLimit); v != nil {
		scConfig.HistoricalProofRateLimit = cast.ToFloat64(v)
	}
	if v := appOpts.Get(FlagSCHistoricalProofBurst); v != nil {
		scConfig.HistoricalProofBurst = cast.ToInt(v)
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
	ssConfig.EnableReadWriteMetrics = cast.ToBool(appOpts.Get(FlagSSReadWriteMetrics))

	// EVM optimization fields (embedded in SS config)
	ssConfig.EVMDBDirectory = cast.ToString(appOpts.Get(FlagEVMSSDirectory))
	ssConfig.SeparateEVMSubDBs = cast.ToBool(appOpts.Get(FlagEVMSSSeparateDBs))
	ssConfig.EVMSplit = cast.ToBool(appOpts.Get(FlagEVMSSSplit))
	return ssConfig
}
