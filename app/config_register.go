package app

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-db/config"
)

// The names these sections have in the configuration key space.
const (
	LightInvarianceSectionName = "light_invariance"
	GenesisSectionName         = "genesis"
	StateStoreSectionName      = "state-store"
	StateCommitSectionName     = "state-commit"
)

// genesisSchema declares the keys the genesis import reader resolves.
//
// A schema and not a transport: nothing decodes into it. The type the reader fills is
// genesistypes.GenesisImportConfig, which carries no mapstructure tags at all, so no key can be
// derived from it, and it lives in a tree this repository does not change. Declaring the spelling here
// is what lets the registry name the keys the reader looks up. Nothing keeps the two together by
// construction, so a test writes a value under each key and asks the reader which setting it reached.
type genesisSchema struct {
	StreamImport bool   `mapstructure:"stream-import"`
	ImportFile   string `mapstructure:"import-file"`
}

// Registration puts this package's configuration sections in the registry.
//
// The owning package registers its own sections, so the struct, the defaults and the keys all come
// from one place and cannot drift apart. The keys derive from the mapstructure tags, which is what
// makes the registry's spelling and this package's flag constants the same strings.
func init() {
	registry.RegisterSection(LightInvarianceSectionName, &LightInvarianceConfig{}, lightInvarianceBaseline)
	registry.RegisterSection(GenesisSectionName, &genesisSchema{}, genesisBaseline)
	registry.RegisterSection(StateStoreSectionName, &stateStoreSchema{}, stateStoreBaseline)
	registry.RegisterSection(StateCommitSectionName, &stateCommitSchema{}, stateCommitBaseline)
}

// lightInvarianceBaseline is what this section resolves to for a node that has written nothing.
//
// The same value for every mode, and on. The check compares the bank module's recorded total supply
// against what the store holds, which is a correctness property of every node rather than of one
// kind, so a mode-varying baseline would stop some nodes noticing that they had diverged.
func lightInvarianceBaseline(registry.Mode) any { return DefaultLightInvarianceConfig }

// genesisBaseline is what this section resolves to for a node that has written nothing.
//
// Read out of the reader's own default rather than written again here, so a changed default moves both
// at once and this states only which key carries which setting. The same values for every mode:
// streaming a genesis file is what an operator does to import a chain's existing state, and no node
// mode implies it.
func genesisBaseline(registry.Mode) any {
	return genesisSchema{
		StreamImport: DefaultGenesisConfig.StreamGenesisImport,
		ImportFile:   DefaultGenesisConfig.GenesisStreamFile,
	}
}

// stateStoreSchema declares the keys parseSSConfigs resolves.
//
// A schema and not a transport: nothing decodes into it. config.StateStoreConfig carries mapstructure
// tags of its own, and every one of them names something other than the key the reader looks up: the
// field for state-store.ss-enable is tagged "enable", so deriving keys from that type would declare
// thirteen keys and none of the eleven an operator writes. Declaring the spelling here is what makes
// the registry name the keys the reader resolves.
//
// config.StateStoreConfig also carries KeepLastVersion and UseDefaultComparer, which no key reaches.
// The reader never looks them up, so they stay at whatever the defaults struct holds, and giving them
// keys would declare two settings a written value could not change.
type stateStoreSchema struct {
	Enable                 bool   `mapstructure:"ss-enable"`
	DBDirectory            string `mapstructure:"ss-db-directory"`
	Backend                string `mapstructure:"ss-backend"`
	AsyncWriteBuffer       int    `mapstructure:"ss-async-write-buffer"`
	KeepRecent             int    `mapstructure:"ss-keep-recent"`
	PruneIntervalSeconds   int    `mapstructure:"ss-prune-interval"`
	ImportNumWorkers       int    `mapstructure:"ss-import-num-workers"`
	EnableReadWriteMetrics bool   `mapstructure:"ss-enable-read-write-metrics"`
	EVMDBDirectory         string `mapstructure:"evm-ss-db-directory"`
	SeparateEVMSubDBs      bool   `mapstructure:"evm-ss-separate-dbs"`
	EVMSplit               bool   `mapstructure:"evm-ss-split"`
}

// stateStoreBaseline is what this section resolves to for a node that has written nothing.
//
// The declared defaults, which is what seid init writes into app.toml: srvconfig.DefaultConfig calls
// config.DefaultStateStoreConfig for the template it renders. So a generated sei.toml reproduces a
// freshly initialised node.
//
// That is not what parseSSConfigs produces for a configuration with these keys missing. It assigns
// every field straight from a lookup with no check that the key was present, so an absent key resolves
// to zero and clobbers the default beside it: the store reads as disabled, with no backend, keeping
// every version, and committing synchronously. A node whose app.toml predates one of these keys runs
// the clobbered value today and runs the declared default once this section is declared.
// testdata/state-store.absent.golden is the record of exactly which keys that is, and guarding the
// reads in parseSSConfigs is what empties it.
//
// The same values for every mode. How much history a node keeps is an operator's decision about disk,
// and the modes do not imply one; an archive node's intent is expressed by keeping everything, which is
// a value it writes rather than a default it inherits.
func stateStoreBaseline(registry.Mode) any {
	live := config.DefaultStateStoreConfig()
	return stateStoreSchema{
		Enable:                 live.Enable,
		DBDirectory:            live.DBDirectory,
		Backend:                live.Backend,
		AsyncWriteBuffer:       live.AsyncWriteBuffer,
		KeepRecent:             live.KeepRecent,
		PruneIntervalSeconds:   live.PruneIntervalSeconds,
		ImportNumWorkers:       live.ImportNumWorkers,
		EnableReadWriteMetrics: live.EnableReadWriteMetrics,
		EVMDBDirectory:         live.EVMDBDirectory,
		SeparateEVMSubDBs:      live.SeparateEVMSubDBs,
		EVMSplit:               live.EVMSplit,
	}
}

// stateCommitFlatKVSchema declares the one flat key-value setting that has a key of its own.
//
// A nested segment, because the key is state-commit.flatkv.enable-read-write-metrics. The rest of the
// flat key-value configuration has no keys: nothing reads them from configuration, so declaring them
// would give an operator two dozen settings a written value could not change.
type stateCommitFlatKVSchema struct {
	EnableReadWriteMetrics bool `mapstructure:"enable-read-write-metrics"`
}

// stateCommitSchema declares the keys parseSCConfigs resolves.
//
// A schema and not a transport: nothing decodes into it. config.StateCommitConfig nests its settings
// under MemIAVLConfig, FlatKVConfig and HashLogger, and the keys the reader looks up are flat names on
// the section itself, so no derivation from that type produces them. It also holds many settings with
// no key at all, which stay at whatever the defaults hold.
//
// The write mode is a plain string rather than the reader's own named type. A named string type does
// not survive the conversion the reader applies to a written value, so declaring one would put a value
// in the configuration that the reader cannot read.
type stateCommitSchema struct {
	Enable                     bool                    `mapstructure:"sc-enable"`
	Directory                  string                  `mapstructure:"sc-directory"`
	AsyncCommitBuffer          int                     `mapstructure:"sc-async-commit-buffer"`
	SnapshotKeepRecent         uint32                  `mapstructure:"sc-keep-recent"`
	SnapshotInterval           uint32                  `mapstructure:"sc-snapshot-interval"`
	SnapshotMinTimeInterval    uint32                  `mapstructure:"sc-snapshot-min-time-interval"`
	SnapshotWriterLimit        int                     `mapstructure:"sc-snapshot-writer-limit"`
	SnapshotPrefetchThreshold  float64                 `mapstructure:"sc-snapshot-prefetch-threshold"`
	SnapshotWriteRateMBps      int                     `mapstructure:"sc-snapshot-write-rate-mbps"`
	HistoricalProofMaxInFlight int                     `mapstructure:"sc-historical-proof-max-inflight"`
	HistoricalProofRateLimit   float64                 `mapstructure:"sc-historical-proof-rate-limit"`
	HistoricalProofBurst       int                     `mapstructure:"sc-historical-proof-burst"`
	WriteMode                  string                  `mapstructure:"sc-write-mode"`
	WriteModeEnableAuto        bool                    `mapstructure:"sc-write-mode-enable-auto"`
	HashLoggerEnable           bool                    `mapstructure:"sc-hash-logger-enable"`
	HashLoggerDirectory        string                  `mapstructure:"sc-hash-logger-directory"`
	HashLoggerBlocksToRetain   uint                    `mapstructure:"sc-hash-logger-blocks-to-retain"`
	HashLoggerTargetFileSize   uint                    `mapstructure:"sc-hash-logger-target-file-size"`
	HashLoggerMaxDiskSize      uint                    `mapstructure:"sc-hash-logger-max-disk-size"`
	FlatKV                     stateCommitFlatKVSchema `mapstructure:"flatkv"`
}

// Validate reports whether this configuration is usable.
//
// The write mode names one of a fixed set of routing behaviours, and parseSCConfigs stops the node with
// a panic when it cannot recognise the written name. Stating the rule here is what lets a diagnostic
// refuse the file first, so an operator finds out before a restart rather than during one. An empty
// name is not refused: that is how an absent key arrives, and the reader keeps its default for it.
func (s stateCommitSchema) Validate() error {
	if s.WriteMode == "" {
		return nil
	}
	if _, err := config.ParseSCWriteMode(s.WriteMode); err != nil {
		return fmt.Errorf("sc-write-mode %q is not a write mode this binary recognises: %w", s.WriteMode, err)
	}
	return nil
}

// stateCommitBaseline is what this section resolves to for a node that has written nothing.
//
// The declared defaults, which is what seid init writes into app.toml. Eighteen of the twenty reads in
// parseSCConfigs already check that the key was present, so for those the declared default is also what
// an absent key resolves to today. The two that do not are sc-enable and sc-directory, and
// testdata/state-commit.absent.golden records what changes for a node missing either.
//
// sc-enable is the one that matters. An absent key reads as false, and SetupSeiDB stops the node when
// state commitment is off, so no running node has that key missing. Resolving it to true is what every
// working node already has in its app.toml.
//
// The same values for every mode. How often a node snapshots and how much proof history it serves are
// decisions about disk and load that an operator writes down.
func stateCommitBaseline(registry.Mode) any {
	live := config.DefaultStateCommitConfig()
	return stateCommitSchema{
		Enable:                     live.Enable,
		Directory:                  live.Directory,
		AsyncCommitBuffer:          live.MemIAVLConfig.AsyncCommitBuffer,
		SnapshotKeepRecent:         live.MemIAVLConfig.SnapshotKeepRecent,
		SnapshotInterval:           live.MemIAVLConfig.SnapshotInterval,
		SnapshotMinTimeInterval:    live.MemIAVLConfig.SnapshotMinTimeInterval,
		SnapshotWriterLimit:        live.MemIAVLConfig.SnapshotWriterLimit,
		SnapshotPrefetchThreshold:  live.MemIAVLConfig.SnapshotPrefetchThreshold,
		SnapshotWriteRateMBps:      live.MemIAVLConfig.SnapshotWriteRateMBps,
		HistoricalProofMaxInFlight: live.HistoricalProofMaxInFlight,
		HistoricalProofRateLimit:   live.HistoricalProofRateLimit,
		HistoricalProofBurst:       live.HistoricalProofBurst,
		WriteMode:                  string(live.WriteMode),
		WriteModeEnableAuto:        live.WriteModeEnableAuto,
		HashLoggerEnable:           live.HashLogger.Enable,
		HashLoggerDirectory:        live.HashLogger.Directory,
		HashLoggerBlocksToRetain:   live.HashLogger.BlocksToRetain,
		HashLoggerTargetFileSize:   live.HashLogger.TargetFileSize,
		HashLoggerMaxDiskSize:      live.HashLogger.MaxDiskSize,
		FlatKV: stateCommitFlatKVSchema{
			EnableReadWriteMetrics: live.FlatKVConfig.EnableReadWriteMetrics,
		},
	}
}
