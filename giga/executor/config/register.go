package config

import (
	"github.com/sei-protocol/sei-chain/config/registry"
)

// SectionName is this section's name in the configuration key space.
//
// The same prefix the flag constants already use, so the derived keys are the keys this package's
// reader resolves rather than a second spelling of them.
const SectionName = "giga_executor"

// Registration puts this section in the configuration registry.
//
// The owning package registers its own section, so the struct, the defaults and the keys all come
// from one place and cannot drift apart. The dotted keys derive from the mapstructure tags, which is
// what makes the registry's spelling and this package's flag constants the same strings.
func init() {
	registry.RegisterSection(SectionName, &Config{}, baseline)
}

// baseline is what this section resolves to for a node that has written nothing.
//
// The same values for every mode, because that is what a node runs today. A mode-varying default
// would change what an archive node does, and that is a decision about how the executor should behave
// rather than a consequence of describing it here.
func baseline(registry.Mode) any { return DefaultConfig }
