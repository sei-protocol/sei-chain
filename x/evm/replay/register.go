package replay

import "github.com/sei-protocol/sei-chain/config/registry"

// SectionName is this section's name in the configuration key space.
const SectionName = "eth_replay"

// Registration puts this package's configuration section in the registry.
//
// The owning package registers its own section, so the struct, the values and the keys come from one
// place. This section's mapstructure tags already spell the keys its reader resolves, so the registry
// derives what a node reads rather than restating them.
//
// One of the four keys is written into app.toml under a name nothing reads. The template renders
// eth_replay_contract_state_checks and the reader looks up contract_state_checks, so the declared key is
// the one a value reaches a reader through.
func init() {
	registry.RegisterSection(SectionName, &Config{}, defaults)
}

// defaults is what the seid init command writes for a node of this kind.
//
// The same values for every mode, and replay off. Turning it on makes a node replay recorded chain data
// from an endpoint instead of following the chain, and the endpoint is a fixed third-party address, so no
// kind of node implies it. Construction opens a client for that address without reaching it, which is why
// an unreachable endpoint surfaces during replay rather than at startup.
func defaults(registry.Mode) any { return DefaultConfig }
