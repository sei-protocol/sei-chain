package config

import "github.com/sei-protocol/sei-chain/config/registry"

// SectionName is this section's name in the configuration key space.
const SectionName = "evm"

// Registration puts this package's configuration section in the registry.
//
// The owning package registers its own section, so the struct, the values and the keys come from one
// place. This section's mapstructure tags already spell the keys its reader resolves, all fifty-seven of
// them, so the registry derives what a node reads rather than restating a list this long.
func init() {
	registry.RegisterSection(SectionName, &Config{}, defaults)
}

// defaults is what this section resolves to for a node that has written nothing.
//
// The declared defaults, unchanged by mode, because that is what such a node runs: nothing consults the
// node's kind while reading these keys, so a file missing them serves both interfaces whatever kind of
// node it is.
//
// A node seid init provisioned is a different case and needs no help from here. That path writes the two
// interface toggles per mode, closing them for a validator and a seed, so those nodes carry written values
// and a written value is what resolves.
//
// Two of these values come from the machine rather than from a decision: the simulation call limit is the
// processor count and the worker pool is twice it, capped. They describe the host that asked, so a caller
// that renders them into a file carries one host's sizing to whatever reads that file next.
func defaults(registry.Mode) any { return DefaultConfig }
