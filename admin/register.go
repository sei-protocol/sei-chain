package admin

import (
	"github.com/sei-protocol/sei-chain/config/registry"
)

// SectionName is this section's name in the configuration key space.
const SectionName = "admin_server"

// Registration puts this section in the configuration registry.
//
// The keys derive from the mapstructure tags, and the reader resolves the same strings through the
// constants beside it, so a rename moves one occurrence and the test holds the two together.
func init() {
	registry.RegisterSection(SectionName, &Config{}, defaults)
}

// defaults is what this section resolves to for a node that has written nothing.
func defaults(registry.Mode) any { return DefaultConfig }
