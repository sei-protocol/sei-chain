package config

import (
	"github.com/sei-protocol/sei-chain/config/registry"
)

// SectionName is this section's name in the configuration key space.
const SectionName = "evm"

// Registration puts this section in the configuration registry.
//
// The owning package registers its own section, so the struct, the defaults and the keys all come from
// one place. This section's mapstructure tags already produce the keys its reader resolves, so the
// registry derives what a node actually reads without anything being restated here.
func init() {
	registry.RegisterSection(SectionName, &Config{}, baseline)
}

// baseline is what this section resolves to for a node that has written nothing.
func baseline(mode registry.Mode) any { return DefaultConfigForMode(mode) }

// DefaultConfigForMode is this section's defaults for a node mode.
//
// The one definition of what a mode implies for this section. seid init reads it to write app.toml and
// the registry reads it to resolve an absent key, so the two cannot disagree about what a validator is.
// Before this existed they were separate: DefaultConfig said one thing and a setter in app/params
// applied another at init time, with nothing comparing them.
//
// Only the two keys a mode actually changes differ. Everything else is DefaultConfig, so a mode cannot
// quietly acquire a difference nobody declared.
func DefaultConfigForMode(mode registry.Mode) Config {
	cfg := DefaultConfig
	// The RPC services a fullnode serves. A validator and a seed do not serve them, which is what
	// keeps a validator's RPC ports closed unless an operator opens them.
	serves := registry.IsFullnodeMode(mode)
	cfg.HTTPEnabled = serves
	cfg.WSEnabled = serves
	return cfg
}
