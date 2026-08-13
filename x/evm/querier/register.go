package querier

import (
	"github.com/sei-protocol/sei-chain/config/registry"
)

// SectionName is this section's name in the configuration key space.
const SectionName = "evm_query"

// Registration puts this section in the configuration registry.
//
// The owning package registers its own section, so the struct, the defaults and the keys all come
// from one place and cannot drift apart. The keys derive from the mapstructure tags, which is what
// makes the registry's spelling and this package's flag constants the same strings.
func init() {
	registry.RegisterSection(SectionName, &Config{}, baseline)
}

// baseline is what this section resolves to for a node that has written nothing.
//
// The same value for every mode. The limit bounds work a contract can ask the EVM to do inside a
// query, and every node answers the same queries.
func baseline(registry.Mode) any { return DefaultConfig }
