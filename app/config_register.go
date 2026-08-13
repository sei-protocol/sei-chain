package app

import (
	"github.com/sei-protocol/sei-chain/config/registry"
)

// LightInvarianceSectionName is this section's name in the configuration key space.
const LightInvarianceSectionName = "light_invariance"

// Registration puts this package's configuration sections in the registry.
//
// The owning package registers its own sections, so the struct, the defaults and the keys all come
// from one place and cannot drift apart. The keys derive from the mapstructure tags, which is what
// makes the registry's spelling and this package's flag constants the same strings.
func init() {
	registry.RegisterSection(LightInvarianceSectionName, &LightInvarianceConfig{}, lightInvarianceBaseline)
}

// lightInvarianceBaseline is what this section resolves to for a node that has written nothing.
//
// The same value for every mode, and on. The check compares the bank module's recorded total supply
// against what the store holds, which is a correctness property of every node rather than of one
// kind, so a mode-varying baseline would stop some nodes noticing that they had diverged.
func lightInvarianceBaseline(registry.Mode) any { return DefaultLightInvarianceConfig }
