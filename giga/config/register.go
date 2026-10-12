package config

import "github.com/sei-protocol/sei-chain/config/registry"

// SectionName is this section's name in the configuration key space.
const SectionName = "giga"

// Registration puts this section in the configuration registry.
//
// The keys derive from the mapstructure tags, and the reader resolves the same strings through the
// constants beside it, so a rename moves one occurrence and the test holds the two together.
func init() {
	registry.RegisterSection(SectionName, &Config{}, defaults)
}

// defaults is what the seid init command writes for a node of this kind.
//
// The same values for every mode: the store layout follows the node's own mode at start-up
// through the empty storage mode, so no default here has to restate that rule.
func defaults(registry.Mode) any { return DefaultConfig }
