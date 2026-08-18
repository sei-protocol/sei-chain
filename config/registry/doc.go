// Package registry is the single declaration point for a stable configuration key.
//
// A key enters here once, as a field on its owning package's section struct, registered with
// a default that may vary by node mode. The dotted key identity, the canonical environment
// spelling and the read site all derive from that one registration, so nobody hand-writes a
// flag, an environment binding or a cast-heavy reader per key.
//
// Defaults are not state. They live in the binary, may change between releases, and never
// mutate an existing configuration or require a migration. A written value is a commitment
// the system never rewrites; an absent key tracks whatever default the running binary
// carries.
//
// Resolve reduces a set of named layers to one value per declared key, in the order Precedence
// declares, and records which layer each value came from. It answers for declared keys only, so
// it serves a diagnostic or an authoring check rather than the boot: what a running node reads
// stays a source carrying every resolved key, whether a section declares it or not.
//
// spec_test.go asserts each of these rules as its own test, named for the property it holds.
package registry
