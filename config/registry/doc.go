// Package registry is the single declaration point for a stable configuration key.
//
// A key enters here once, as a field on its owning package's section struct, registered with
// a baseline that may vary by node mode. The dotted key identity, the canonical environment
// spelling, the schema fingerprint and the read site all derive from that one registration,
// so nobody hand-writes a flag, an environment binding or a cast-heavy reader per key.
//
// Baselines are not state. They live in the binary, may change between releases, and never
// mutate an existing configuration or require a migration. A written value is a commitment
// the system never rewrites; an absent key tracks whatever baseline the running binary
// carries.
//
// The acceptance gates in spec_test.go define this package's scope and are red until it is
// built. They sit behind the configspec build tag; removing the tag is what completes PR4.
package registry
