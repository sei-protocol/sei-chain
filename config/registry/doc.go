// Package registry is the single declaration point for a stable configuration key.
//
// A key enters here once, as a field on its owning package's section struct, registered with a
// default that may vary by node mode. The dotted key identity, the canonical environment spelling
// and the read site all derive from that one registration, so nobody hand-writes a flag, an
// environment binding or a cast-heavy reader per key.
//
// # Three Statements Per Key
//
// A configuration key in this tree needs three things written down: the reader's lookup, a dotted
// string at the point the value is consumed; a flag binding, if an operator can pass the value on the
// command line; and an environment spelling, derived by upper-casing and substituting separators.
//
// A key that does not register here states all three separately, and nothing ties them together, so
// they drift independently. A rename can move the reader and leave the flag behind, and the result is
// a key an operator sets that reaches nothing. The characterization suite in testutil/configtest pins
// several such mismatches, which is how they were found rather than reported.
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
// Resolve reduces a node's configuration sources to one value per declared key: its defaults, then
// its file, then the environment, then its flags. The precedence is stated once, inside Resolve, and
// no exported value repeats it.
//
// Alongside the values it reports which keys something other than the defaults supplied, and which
// keys a source carried that no section declares. The first is what a diff renders, since a written
// value and a default are otherwise indistinguishable once merged. The second is why a typo in an
// operator's file is visible rather than silently dropped.
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
// Unusable means anything that would leave a key an operator writes reaching nothing: a field with no
// tag, an unexported field carrying a tag, two fields declaring one path, a struct that declares no
// key, a struct that contains itself, and two keys that collapse onto one environment variable.
//
// A key segment is also refused if it is upper-case, or if it carries a dot or a space. That rule
// holds for the section name and for a field's tag alike, since both become segments of the same
// dotted key and answer to the same sources.
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
