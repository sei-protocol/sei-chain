package admin

import (
	"github.com/sei-protocol/sei-chain/config/registry"
)

// SectionName is this section's name in the configuration key space.
const SectionName = "admin_server"

// Registration puts this section in the configuration registry.
//
// The owning package registers its own section, so the struct, the defaults and the keys all come
// from one place and cannot drift apart. Config states a rule of its own, so the registry also
// checks a resolved configuration against it, and the check an operator gets is the same one the
// boot performs rather than a second statement of it.
func init() {
	registry.RegisterSection(SectionName, &Config{}, baseline)
}

// baseline is what this section resolves to for a node that has written nothing.
//
// The same values for every mode. Which node this is has no bearing on whether an operator wants a
// local administrative surface, and varying it here would turn one mode's nodes on.
func baseline(registry.Mode) any { return DefaultConfig }
