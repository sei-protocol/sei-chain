package config

import (
	"github.com/sei-protocol/sei-chain/config/registry"
)

// SectionName is this section's name in the configuration key space.
//
// The same prefix the flag constants already use, so the derived keys are the keys this package's reader
// resolves rather than a second spelling of them.
const SectionName = "giga_executor"

// Registration puts this section in the configuration registry.
//
// The owning package registers its own section, so the struct, the values and the keys come from one place
// and cannot drift apart. The dotted keys derive from the mapstructure tags, which is what makes the
// registry's spelling and this package's flag constants the same strings.
func init() {
	registry.RegisterSection(SectionName, &Config{}, defaults)
}

// defaults is what the seid init command writes for a node of this kind.
//
// The same values for every mode. Nothing in the binary makes either setting follow from what kind of
// node is asking, so a default that varied here would be this section inventing a rule rather than
// stating one.
func defaults(registry.Mode) any { return DefaultConfig }
