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
