// Package appopts installs resolved configuration into the source a booting node reads. Declared keys
// are written at override precedence; every other key is left for the source to answer as before.
package appopts

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/viper"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// Report says where each key in the resulting configuration comes from.
type Report struct {
	// Installed are the declared keys written at override precedence, sorted.
	Installed []string
	// Passthrough are keys the source enumerates that no section declares, sorted.
	Passthrough []string
	// Added are declared keys the source did not enumerate, sorted.
	Added []string
}

// Install writes every resolved value into target at override precedence and reports what it found.
func Install(target *viper.Viper, resolved registry.Resolved) (Report, error) {
	if target == nil {
		return Report{}, fmt.Errorf("no configuration source to install into")
	}
	// Enumerated once so the refusal and the report agree on what the source carries.
	enumerated := enumerate(target)
	env := theEnvironment()
	if err := refuseNotLowerCase(resolved); err != nil {
		return Report{}, err
	}
	if err := refuseUnwritable(target, resolved, enumerated, env); err != nil {
		return Report{}, err
	}

	// Described before writing, since writing makes every declared key enumerable.
	report := describe(target, resolved, enumerated, env)
	for key, value := range resolved.Values {
		target.Set(key, value)
	}
	return report, nil
}

// enumerate returns the keys the source lists, which are lower case.
func enumerate(target *viper.Viper) map[string]bool {
	keys := target.AllKeys()
	out := make(map[string]bool, len(keys))
	for _, key := range keys {
		out[key] = true
	}
	return out
}

// theEnvironment returns the names of the non-empty environment variables, the ones AutomaticEnv reads.
// The source cannot list them, so a key they answer is missing from enumerate.
func theEnvironment() map[string]bool {
	out := map[string]bool{}
	for _, entry := range os.Environ() {
		if name, value, ok := strings.Cut(entry, "="); ok && value != "" {
			out[name] = true
		}
	}
	return out
}

// envName returns the variable target's AutomaticEnv reads for key.
func envName(target *viper.Viper, key string) string {
	return registry.EnvNameUnder(target.GetEnvPrefix(), key)
}

// refuseNotLowerCase rejects a declared key that is not lower case. The source lower-cases keys on
// write, so such a key would evade refuseUnwritable's collision check and be reported twice.
func refuseNotLowerCase(resolved registry.Resolved) error {
	var wrong []string
	for key := range resolved.Values {
		if key != strings.ToLower(key) {
			wrong = append(wrong, key)
		}
	}
	if len(wrong) == 0 {
		return nil
	}
	sort.Strings(wrong)
	return fmt.Errorf("these declared keys are not lower case, and a configuration source stores a key "+
		"lower-cased, so each would be written and read under a name nothing declares: %s",
		strings.Join(wrong, ", "))
}

// dottedPrefixes returns every path a key nests under, outermost first. Derived from the key because a
// sorted scan would be fooled by hyphens sorting before dots (grpc, grpc-web.address, grpc.enable).
func dottedPrefixes(key string) []string {
	var out []string
	for i := range key {
		if key[i] == '.' {
			out = append(out, key[:i])
		}
	}
	return out
}

// refuseUnwritable rejects a declared key that is a dotted prefix of another key, or nests under one.
// The source holds one value per path, so writing both would silently lose one of them.
func refuseUnwritable(target *viper.Viper, resolved registry.Resolved, enumerated, env map[string]bool) error {
	var lost []string
	// Each pair says which key is undeclared, because that decides who can fix it.
	note := func(outer, inner string, bothDeclared bool) {
		if bothDeclared {
			lost = append(lost, fmt.Sprintf("%q and %q, both declared", outer, inner))
			return
		}
		lost = append(lost, fmt.Sprintf("%q declared and %q not", outer, inner))
	}

	// A declared key nesting under another declared key or a value the source holds.
	for key := range resolved.Values {
		for _, under := range dottedPrefixes(key) {
			if _, declared := resolved.Values[under]; declared {
				note(under, key, true)
			} else if holdsAValue(target, under) {
				note(key, under, false)
			}
		}
	}
	// An enumerated key nesting under a declared key, which the declared value would shadow.
	for key := range enumerated {
		if _, declared := resolved.Values[key]; declared {
			continue
		}
		for _, under := range dottedPrefixes(key) {
			if _, declared := resolved.Values[under]; declared {
				note(under, key, false)
			}
		}
	}
	lost = append(lost, variablesUnderADeclaredKey(target, resolved, enumerated, env)...)
	if len(lost) == 0 {
		return nil
	}
	sort.Strings(lost)
	return fmt.Errorf("these key pairs cannot both be installed, because one names a path the other "+
		"nests under and the source holds one value per path: %s. An undeclared key is an operator's to "+
		"rename; a declared one changes only in a release", strings.Join(lost, ", "))
}

// variablesUnderADeclaredKey describes each environment variable that nests under a declared key, which
// the declared value would shadow. Variable names are matched forward from the declared key, since a
// name cannot be mapped back: the replacer turns both dots and hyphens into underscores. A variable that
// is the name of a declared or enumerated key is that key's own, and is not a collision.
func variablesUnderADeclaredKey(target *viper.Viper, resolved registry.Resolved,
	enumerated, env map[string]bool) []string {
	owned := make(map[string]bool, len(resolved.Values)+len(enumerated))
	for key := range resolved.Values {
		owned[envName(target, key)] = true
	}
	for key := range enumerated {
		owned[envName(target, key)] = true
	}
	var lost []string
	for key := range resolved.Values {
		under := envName(target, key) + "_"
		for name := range env {
			if strings.HasPrefix(name, under) && !owned[name] {
				lost = append(lost, fmt.Sprintf("%q declared and the variable %s nests under it", key, name))
			}
		}
	}
	return lost
}

// holdsAValue reports whether target has key set to something other than a table. It asks the source
// rather than the enumeration so environment values count, and uses IsSet so an unset bound flag's
// default does not.
func holdsAValue(target *viper.Viper, key string) bool {
	if !target.IsSet(key) {
		return false
	}
	switch target.Get(key).(type) {
	case map[string]any, map[any]any:
		return false
	default:
		return true
	}
}

// describe sorts the resolved and enumerated keys into a Report. A declared key its environment variable
// answers is not Added.
func describe(target *viper.Viper, resolved registry.Resolved, enumerated, env map[string]bool) Report {
	var report Report
	for key := range resolved.Values {
		report.Installed = append(report.Installed, key)
		if !enumerated[key] && !env[envName(target, key)] {
			report.Added = append(report.Added, key)
		}
	}
	for key := range enumerated {
		if _, declared := resolved.Values[key]; !declared {
			report.Passthrough = append(report.Passthrough, key)
		}
	}

	sort.Strings(report.Installed)
	sort.Strings(report.Passthrough)
	sort.Strings(report.Added)
	return report
}
