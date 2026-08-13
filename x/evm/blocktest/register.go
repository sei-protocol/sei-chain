package blocktest

import (
	"github.com/sei-protocol/sei-chain/config/registry"
)

// SectionName is this section's name in the configuration key space.
const SectionName = "eth_blocktest"

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
// The same values for every mode. This section drives a test harness against recorded block data,
// which is not something any node mode does by default.
func baseline(registry.Mode) any { return DefaultConfig }
