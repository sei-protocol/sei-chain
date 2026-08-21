package app

import (
	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/config/registry"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
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
// place and cannot drift apart. The keys derive from mapstructure tags, so a section's spelling and its
// reader's own constants stay the same strings.
func init() {
	registry.RegisterSection(LightInvarianceSectionName, &LightInvarianceConfig{}, lightInvarianceDefaults)
	registry.RegisterSection(GenesisSectionName, &genesisSchema{}, genesisDefaults)
	registry.RegisterSection(StateStoreSectionName, &stateStoreSchema{}, stateStoreDefaults)
	registry.RegisterSection(StateCommitSectionName, &stateCommitSchema{}, stateCommitDefaults)
}

// lightInvarianceDefaults is what this section resolves to for a node that has written nothing.
//
// The same value for every mode, and on. What the check compares is a property of every node rather than
// of one kind, so a mode that resolved it off would stop those nodes noticing they had diverged.
func lightInvarianceDefaults(registry.Mode) any { return DefaultLightInvarianceConfig }

// genesisSchema declares the keys the genesis import reader resolves.
//
// A schema and not a transport: nothing decodes into it. The type the reader fills is
// genesistypes.GenesisImportConfig, which carries no mapstructure tags at all, so no key can be derived
// from it. Declaring the spelling here is what lets the registry name the keys the reader looks up.
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
// Answered per mode, because two of these settings mean something different depending on what kind of node
// asks. An archive node exists to keep history, so it keeps every version; a validator and a seed serve no
// queries, so the store is off for them. Both come from the mode rules the binary already states rather
// than being written again here, so a change to those rules moves this too.
//
// This is the one section here whose declared values are not what its reader produces for a file missing
// the keys, and the divergences are measured rather than described. A test names each one and what a node
// runs today, so a read that gains a presence check has to account for it.
func stateStoreDefaults(mode registry.Mode) any {
	server := srvconfig.DefaultConfig()
	params.SetAppConfigByMode(server, params.NodeMode(mode))
	live := server.StateStore
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

// stateCommitFlatKVSchema declares the one flat key-value key this package's reader resolves.
//
// A nested segment, because the key is state-commit.flatkv.enable-read-write-metrics. Four further keys
// under that name are read by the Cosmos server's own configuration reader and not by this one, so they
// belong to whoever registers that reader's section rather than to this one.
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
// The declared defaults. Two of them are not what this section's reader produces for a file missing the
// key, and a test names which two and what a node runs instead.
//
// The same values for every mode. How often a node snapshots and how much proof history it serves are
// decisions about disk and load that an operator writes down, and nothing in the binary makes either
// follow from what kind of node is asking.
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
