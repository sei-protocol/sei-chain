// Package configtest is the hermetic harness the configuration
// characterization and fuzz suites boot through.
//
// The legacy seid configuration path resolves values from four layers (in-code
// defaults, config.toml/app.toml, environment variables, cobra flags) through
// several viper instances whose env prefixes differ, and it materializes files
// as a side effect of reading them. Pinning that behavior in a test therefore
// requires controlling more than the arguments to the function under test: the
// process environment, $HOME, and the executable basename all feed the result.
//
// This package supplies the three things every such test needs, and nothing
// else:
//
//   - Isolate pins the process environment to a known-empty state so a stray
//     var on the developer's machine cannot change a resolved value. It covers
//     the environment and $HOME only; see its doc for the two package-global
//     mutations the legacy read path makes that it deliberately leaves alone.
//   - Home builds a fixture node directory whose config/ contents the test
//     controls byte for byte.
//   - Dump renders a resolved view as deterministic, diff-friendly text,
//     carrying the concrete Go type of every leaf so a string "5" is never
//     mistaken for an int 5.
//
// It deliberately imports no sei-chain package. The surfaces under test live in
// app, cmd/seid/cmd, sei-cosmos and sei-db; keeping this package free of them
// lets their in-package tests import it without an import cycle.
package configtest

// AppOpts is a servertypes.AppOptions backed by a plain map: the transport every
// section reader consumes, with none of viper's resolution behavior. Reader
// tests use it to state exactly which keys are present and what raw value each
// carries, including a key explicitly present with a nil value (which viper can
// produce and which reads differently from an absent key).
//
// It satisfies both servertypes.AppOptions and sei-db config.AppOptions
// structurally, so it needs no import of either.
type AppOpts map[string]any

// Get returns the value recorded for key, or nil when the key is absent.
func (o AppOpts) Get(key string) any { return o[key] }

// Clone returns a copy, so a caller can derive a variant scenario without
// mutating the original.
func (o AppOpts) Clone() AppOpts {
	out := make(AppOpts, len(o))
	for k, v := range o {
		out[k] = v
	}
	return out
}

// With returns a copy with key set to value.
func (o AppOpts) With(key string, value any) AppOpts {
	out := o.Clone()
	out[key] = value
	return out
}
