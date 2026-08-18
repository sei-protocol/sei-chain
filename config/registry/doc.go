// Package registry is the single declaration point for a stable configuration key.
//
// A key enters here once, as a field on its owning package's section struct, registered with a
// default that may vary by node mode. The dotted key identity, the canonical environment spelling
// and the read site all derive from that one registration, so nobody hand-writes a flag, an
// environment binding or a cast-heavy reader per key.
//
// # Three Statements Per Key
//
// A configuration key in this tree needs three things written down, and today they are written
// separately: the reader's lookup, a dotted string at the point the value is consumed; a flag
// binding, if an operator can pass the value on the command line; and an environment spelling,
// derived by upper-casing and substituting separators.
//
// Nothing ties the three together, so they drift independently. A rename can move the reader and
// leave the flag behind, and the result is a key an operator sets that reaches nothing. Several such
// mismatches already exist and are pinned by the characterization suite in testutil/configtest,
// which is how they were found rather than reported.
//
// Registering a section states the key once, so editing the tag moves all three together and there
// is no second place to forget. The tag is still a string literal, and a rename is still a rename of
// text; what changes is that there is one occurrence of it instead of three.
//
// # Declaration
//
// A section registers the struct its reader already uses:
//
//	func init() {
//		registry.RegisterSection(SectionName, &Config{}, defaults)
//	}
//
// The prototype is read for its fields and their tags and never for its values. It is deliberately
// the reader's own struct rather than a copy, because a second struct would be a second statement of
// the same key set and the two would disagree the first time somebody edited one. Where the upstream
// type carries fields configuration cannot address, a section may declare against a struct written
// for the purpose, and that struct should carry a comment saying why the reader's own could not
// serve.
//
// The third argument answers per node mode, because a validator and a seed node do not default
// alike.
//
// # Defaults
//
// Defaults are not state. They live in the binary, may change between releases, and never mutate an
// existing configuration or require a migration. A written value is a commitment the system never
// rewrites, and an absent key tracks whatever default the running binary carries. An operator who
// writes a value keeps it across an upgrade, and an operator who writes nothing follows the binary.
//
// # Resolution
//
// Resolve reduces a set of named layers to one value per declared key, in the order Precedence
// declares, and records which layer each value came from. The order comes from Precedence rather
// than from the order layers are passed, so a caller cannot change the answer by reordering its
// arguments.
//
// The provenance is why this exists rather than a map merge. Merging layers inside one source
// produces the right value and loses where it came from, so an operator whose file says one thing
// and whose node does another has no way to find out why. Here the winning layer is recorded, so a
// diagnostic can name it.
//
// Resolve either answers for every declared key or returns an error naming what it could not answer
// for. A caller is never handed a resolution with a hole in it.
//
// # Registration Never Panics
//
// A registration this package cannot use is recorded as a Defect and the section is not registered.
// Defects returns them, and a section's own test turns them into a failure. Panicking would run
// during the package initialisation of something every feature imports, so it would take down every
// seid invocation including --help, and it would turn a mistake a test catches into a fleet-wide
// incident.
//
// Unusable means anything that would leave a key an operator writes reaching nothing: a field with
// no tag, a tag that is upper-case or carries a dot or a space, an unexported field carrying a tag,
// two fields declaring one path, a struct that declares no key, a struct that contains itself, and
// two keys that collapse onto one environment variable.
//
// # What This Package Is Not
//
//   - Not the boot. Resolve answers for declared keys only. What a running node reads stays a source
//     carrying every resolved key, whether a section declares it or not.
//   - Not a file format. Nothing here reads or writes a configuration file.
//   - Not a validator. A section may state rules about its own values; this package invents none.
//   - Not wired. No section is registered by this package and no reader is migrated onto it.
//
// # Adding a Section
//
//  1. Give the section a name, and use it as the first segment of every key it declares.
//  2. Register the struct the reader already uses, with a per-mode default.
//  3. Assert the registration produced no Defect.
//  4. Hold the derived key names against the reader, so a key that reaches nothing fails.
//
// Step 4 is the one that earns the rest. A declaration nothing checks against the reader is a second
// statement of the key set, which is the problem this package exists to remove.
//
// spec_test.go asserts each of these rules as its own test, named for the property it holds.
package registry
