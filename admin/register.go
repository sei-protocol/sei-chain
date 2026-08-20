package admin

import (
	"github.com/sei-protocol/sei-chain/config/registry"
)

// SectionName is this section's name in the configuration key space.
const SectionName = "admin_server"

// Registration puts this section in the configuration registry.
//
// The keys derive from the mapstructure tags, so they are admin_server.admin_enabled and
// admin_server.admin_address, which are the strings this package's reader already resolves.
func init() {
	registry.RegisterSection(SectionName, &Config{}, defaults)
}

// defaults is what this section resolves to for a node that has written nothing.
func defaults(registry.Mode) any { return DefaultConfig }
