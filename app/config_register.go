package app

import (
	"github.com/sei-protocol/sei-chain/config/registry"
)

// The names these sections have in the configuration key space.
const (
	LightInvarianceSectionName = "light_invariance"
	GenesisSectionName         = "genesis"
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
