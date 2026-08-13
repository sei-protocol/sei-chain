// Package appopts installs resolved configuration into the source a booting node reads.
//
// It layers rather than replaces. A key a section declares is written at override precedence, so the
// registry's answer wins over the file, the environment and any flag, and no resolution runs when the
// node reads it. Every other key is left exactly as it was, answered by the machinery that answers it
// today.
//
// Layering is what makes the migration possible at all. A key's value can only be resolved ahead of
// the read if its name is known, and a name is known only once a section declares it: an environment
// cannot be enumerated for a prefix, so a value delivered that way under a key nothing declares is
// readable and unlistable at the same time. Building a fresh source from an enumeration would drop
// exactly those values and replace an operator's setting with a code default. Leaving them alone
// cannot, because the code that answers them is unchanged.
//
// The two halves partition the problem rather than overlapping. Declared keys are resolvable because
// their names are known; undeclared keys are delegated because theirs are not. The delegated half
// shrinks as sections are declared, and when the last one lands the source carries a resolved value
// for every key, which is where the design says it ends up.
package appopts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/viper"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// Report says where each key in the resulting configuration comes from.
type Report struct {
	// Installed are the declared keys written at override precedence, sorted. The registry answers
	// for these.
	Installed []string
	// Passthrough are keys the source enumerates that no section declares, sorted. These still read
	// as they always have, and they are the migration that remains.
	Passthrough []string
	// Added are declared keys the source did not enumerate, sorted. A node reads these for the first
	// time from the registry.
	Added []string
}

// Install writes every resolved value into the source the boot already built.
//
// Override precedence is what makes a declared key's answer final: viper checks the override layer
// before the file, the environment or a bound flag, so what the registry resolved is what the node
// reads. Nothing else in the source is touched.
func Install(target *viper.Viper, resolved registry.Resolved) (Report, error) {
	if target == nil {
		return Report{}, fmt.Errorf("no configuration source to install into")
	}
	if err := refuseColliding(resolved); err != nil {
		return Report{}, err
	}

	report := describe(target, resolved)
	for key, resolution := range resolved.Keys {
		target.Set(key, resolution.Value)
	}
	return report, nil
}

// describe records where every key comes from, before anything is installed.
//
// Read before the write, because installing a declared key the source did not enumerate makes it
// enumerable, and a report built afterwards could not tell that key from one the source always had.
func describe(target *viper.Viper, resolved registry.Resolved) Report {
	enumerated := map[string]bool{}
	for _, key := range target.AllKeys() {
		enumerated[strings.ToLower(key)] = true
	}

	var report Report
	for key := range resolved.Keys {
		report.Installed = append(report.Installed, key)
		if !enumerated[key] {
			report.Added = append(report.Added, key)
		}
	}
	for key := range enumerated {
		if _, declared := resolved.Keys[key]; !declared {
			report.Passthrough = append(report.Passthrough, key)
		}
	}

	sort.Strings(report.Installed)
	sort.Strings(report.Passthrough)
	sort.Strings(report.Added)
	return report
}

// refuseColliding rejects a declared set holding a key that is a prefix of another.
//
// Two such keys cannot both live in the override layer: whichever is written second turns the other
// into a map or leaves it unreadable, and which one survives depends only on iteration order. A
// section cannot produce such a pair, because keys derive from struct leaves and a leaf is never the
// prefix of another leaf. This is here for a pair arriving from two sections at once.
func refuseColliding(resolved registry.Resolved) error {
	keys := make([]string, 0, len(resolved.Keys))
	for key := range resolved.Keys {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var collisions []string
	for i, key := range keys {
		for j := i + 1; j < len(keys); j++ {
			if !strings.HasPrefix(keys[j], key+".") {
				break
			}
			collisions = append(collisions, key+" and "+keys[j])
		}
	}
	if len(collisions) > 0 {
		return fmt.Errorf("these declared key pairs cannot both be installed, because one is a prefix "+
			"of the other and whichever is written second destroys the first: %s",
			strings.Join(collisions, ", "))
	}
	return nil
}

// Summary is one line an operator or a log can carry.
func (r Report) Summary() string {
	return fmt.Sprintf("%d key(s) resolved from the registry, %d still read as they always have, "+
		"%d declared but absent from the existing configuration",
		len(r.Installed), len(r.Passthrough), len(r.Added))
}

// ArchiveMode is the node mode Tendermint has no name for.
//
// An archive node runs Tendermint as a full node, because Tendermint recognizes validator, full and
// seed and nothing else. seid init writes config.toml that way, which is why config.toml cannot say
// whether a node is an archive and why sei.toml records the mode itself.
const ArchiveMode = "archive"

// TendermintModeFor returns the mode Tendermint runs for a node mode.
//
// Everything but archive runs under its own name. This is the one mapping between the two, and both
// the boot and the doctor read it here rather than each spelling the archive case out.
func TendermintModeFor(nodeMode string) string {
	if nodeMode == ArchiveMode {
		return "full"
	}
	return nodeMode
}

// ReconcileMode reports whether the mode sei.toml records agrees with the one Tendermint runs.
//
// Not equality. An archive node is correctly configured with config.toml saying full, so a check
// demanding the two match would fire on every archive node that is set up properly. What must hold is
// that Tendermint is running the mode this node's mode implies.
//
// A disagreement means the node has one kind of consensus behaviour and another kind's application
// defaults, which is a combination nobody chose.
func ReconcileMode(nodeMode, tendermintMode string) error {
	if nodeMode == "" {
		return fmt.Errorf("sei.toml records no node mode, so there is nothing to compare against " +
			"config.toml")
	}
	if want := TendermintModeFor(nodeMode); !strings.EqualFold(tendermintMode, want) {
		return fmt.Errorf("sei.toml says this is a %q node, which runs Tendermint in %q mode, and "+
			"config.toml says %q. The node would take one kind of consensus behaviour and another "+
			"kind's application defaults", nodeMode, want, tendermintMode)
	}
	return nil
}
