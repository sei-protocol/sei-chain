package querier

import "github.com/sei-protocol/sei-chain/config/registry"

// SectionName is this section's name in the configuration key space.
const SectionName = "evm_query"

// Registration puts this package's configuration section in the registry.
//
// The owning package registers its own section, so the struct, the values and the keys come from one
// place. This section's mapstructure tags already spell the key its reader resolves, so the registry
// derives what a node reads rather than restating it.
func init() {
	registry.RegisterSection(SectionName, &Config{}, defaults)
}

// defaults is what this section resolves to for a node that has written nothing.
//
// The same value for every mode. The limit bounds the work a contract can ask the EVM to do inside a
// query, and every node answers the same queries.
func defaults(registry.Mode) any { return DefaultConfig }
