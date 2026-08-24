package config

import "github.com/sei-protocol/sei-chain/config/registry"

// SectionName is this section's name in the configuration key space.
const SectionName = "evm"

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
// That command applies the same mode rule to this section's own defaults and renders the result, and what
// it renders is passed through rather than refilled from a mode-blind copy, so a declared value here is the
// value that reaches the file.
//
// The two interface toggles are what the rule changes. A full node and an archive node serve queries, which
// is what these interfaces are for; a validator and a seed serve none, and leaving them open would put a
// public request surface on the node that holds a signing key. The rule is read from the registry rather
// than restated, because the package that owns the node mode imports this one and cannot be imported back.
//
// Two values come from the machine rather than from a decision, and they are not one case. The worker pool
// has a portable answer: the pool re-measures whenever the value it is given is not positive, so a file
// carrying zero lets every node size itself, and a caller rendering into a file should write that rather
// than this. The simulation call limit has no portable answer, because zero there is not a request to
// measure but the absence of a limit, and the limit is the only bound on how many simulations a node runs
// at once. Both describe the host that resolved them, so neither travels.
func defaults(mode registry.Mode) any {
	cfg := DefaultConfig
	serves := registry.IsFullnodeMode(mode)
	cfg.HTTPEnabled = serves
	cfg.WSEnabled = serves
	return cfg
}
