package app

import (
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

// Registration puts this package's configuration sections in the registry.
//
// The owning package registers its own sections, so the struct, the values and the keys come from one
// place and cannot drift apart. Three of the four declare a schema rather than the type their reader
// fills, and each says why on the schema itself; the keys still derive from mapstructure tags, so a
// section's spelling and its reader's own constants stay the same strings.
func init() {
	registry.RegisterSection(LightInvarianceSectionName, &LightInvarianceConfig{}, lightInvarianceDefaults)
	registry.RegisterSection(GenesisSectionName, &genesisSchema{}, genesisDefaults)
	registry.RegisterSection(StateStoreSectionName, &stateStoreSchema{}, stateStoreDefaults)
	registry.RegisterSection(StateCommitSectionName, &stateCommitSchema{}, stateCommitDefaults)
}

// lightInvarianceDefaults is what this section resolves to for a node that has written nothing.
//
// The same value for every mode, and on. The check compares the bank module's recorded total supply
// against what the store holds, which is a correctness property of every node rather than of one kind,
// so a mode-varying default would stop some nodes noticing that they had diverged.
func lightInvarianceDefaults(registry.Mode) any { return DefaultLightInvarianceConfig }

// genesisSchema declares the keys the genesis import reader resolves.
//
// A schema and not a transport: nothing decodes into it. The type the reader fills is
// genesistypes.GenesisImportConfig, which carries no mapstructure tags at all, so no key can be derived
// from it. Declaring the spelling here is what lets the registry name the keys the reader looks up, and
// the test holds these tags against the reader's own constants because nothing keeps them together by
// construction.
type genesisSchema struct {
	StreamImport bool   `mapstructure:"stream-import"`
	ImportFile   string `mapstructure:"import-file"`
}

// genesisDefaults is what this section resolves to for a node that has written nothing.
//
// Read out of the reader's own default rather than written again here, so a changed default moves both at
// once and this states only which key carries which setting. The same values for every mode: streaming a
// genesis file is what an operator does to import a chain's existing state, and no node mode implies it.
func genesisDefaults(registry.Mode) any {
	return genesisSchema{
		StreamImport: DefaultGenesisConfig.StreamGenesisImport,
		ImportFile:   DefaultGenesisConfig.GenesisStreamFile,
	}
}

// stateStoreSchema declares the keys parseSSConfigs resolves.
//
// A schema and not a transport: nothing decodes into it. config.StateStoreConfig carries mapstructure
// tags of its own and every one names something other than the key the reader looks up, so deriving from
// that type would declare a set of keys no operator writes. It also holds settings no key reaches, which
// stay at whatever the defaults struct holds; giving them keys would declare settings a written value
// could not change.
type stateStoreSchema struct {
	Enable                 bool   `mapstructure:"ss-enable"`
	DBDirectory            string `mapstructure:"ss-db-directory"`
	Backend                string `mapstructure:"ss-backend"`
	AsyncWriteBuffer       int    `mapstructure:"ss-async-write-buffer"`
	KeepRecent             int    `mapstructure:"ss-keep-recent"`
	PruneIntervalSeconds   int    `mapstructure:"ss-prune-interval"`
	ImportNumWorkers       int    `mapstructure:"ss-import-num-workers"`
	EnableReadWriteMetrics bool   `mapstructure:"ss-enable-read-write-metrics"`
	SnapshotEnable         bool   `mapstructure:"ss-snapshot-enable"`
	EVMDBDirectory         string `mapstructure:"evm-ss-db-directory"`
	SeparateEVMSubDBs      bool   `mapstructure:"evm-ss-separate-dbs"`
	EVMSplit               bool   `mapstructure:"evm-ss-split"`
}

// stateStoreDefaults is what this section resolves to for a node that has written nothing.
//
// The declared defaults, which is what seid init renders into app.toml, so a generated file reproduces a
// freshly initialised node.
//
// That is not what parseSSConfigs produces for a file missing these keys. It starts from the declared
// defaults and then assigns eleven of its twelve fields straight from a lookup with no check that the key
// was present, so an absent key casts to a zero and clobbers the default beside it: the store reads as
// disabled, with no backend, keeping every version, and committing synchronously. Only ss-snapshot-enable
// is guarded, and its own comment at the read says why. So a node whose app.toml predates one of the other
// keys runs the clobbered value today and the declared default once something installs this section, and
// guarding the remaining reads is what makes those the same thing.
func stateStoreDefaults(registry.Mode) any {
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
		SnapshotEnable:         live.SnapshotEnable,
		EVMDBDirectory:         live.EVMDBDirectory,
		SeparateEVMSubDBs:      live.SeparateEVMSubDBs,
		EVMSplit:               live.EVMSplit,
	}
}

// stateCommitFlatKVSchema declares the one flat key-value setting that has a key of its own.
//
// A nested segment, because the key is state-commit.flatkv.enable-read-write-metrics. The rest of the
// flat key-value configuration has no keys: nothing reads them from configuration, so declaring them
// would give an operator settings a written value could not change.
type stateCommitFlatKVSchema struct {
	EnableReadWriteMetrics bool `mapstructure:"enable-read-write-metrics"`
}

// stateCommitSchema declares the keys parseSCConfigs resolves.
//
// A schema and not a transport: nothing decodes into it. config.StateCommitConfig nests its settings
// under MemIAVLConfig, FlatKVConfig and HashLogger, and the keys the reader looks up are flat names on the
// section itself, so no derivation from that type produces them.
//
// The write mode is a plain string rather than the reader's own named type, because the reader parses a
// written name into that type itself. Declaring the named type would have one key answer as a named string
// from these defaults and as a plain one from an operator's file, which is a difference a caller can trip
// over and nothing here needs.
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

// stateCommitDefaults is what this section resolves to for a node that has written nothing.
//
// The declared defaults, which is what seid init renders into app.toml. Eighteen of parseSCConfigs' twenty
// reads already check that the key was present, so for those the declared default is also what an absent
// key resolves to today. The two that do not are sc-enable and sc-directory, and sc-enable is the one that
// matters: an absent key reads as false, and SetupSeiDB stops a node with state commitment off, so no
// running node has that key missing. Resolving it to true is what every working node already has written.
//
// The same values for every mode. How often a node snapshots and how much proof history it serves are
// decisions about disk and load that an operator writes down.
func stateCommitDefaults(registry.Mode) any {
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
