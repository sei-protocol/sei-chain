package blocktest

import "github.com/sei-protocol/sei-chain/config/registry"

// SectionName is this section's name in the configuration key space.
const SectionName = "eth_blocktest"

// Registration puts this package's configuration section in the registry.
//
// The owning package registers its own section, so the struct, the values and the keys come from one
// place. This section's mapstructure tags already spell the keys its reader resolves, so the registry
// derives what a node reads rather than restating them.
func init() {
	registry.RegisterSection(SectionName, &Config{}, defaults)
}

// defaults is what the seid init command writes for a node of this kind.
//
// The same values for every mode. This section drives a harness against recorded block data, which is
// not something any kind of node does while serving a chain.
//
// The data path is a tilde path, and it resolves as written. Whoever opens it expands the tilde, so a
// caller that renders this value into a file writes the same text an operator would.
func defaults(registry.Mode) any { return DefaultConfig }
