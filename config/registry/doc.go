// Package registry is the single declaration point for a stable configuration key.
//
// A section registers the struct its reader already uses, with a default per node mode:
//
//	func init() {
//		registry.RegisterSection(SectionName, &Config{}, defaults)
//	}
//
// Each key's dotted name, environment variable and read site derive from the field's mapstructure
// tag, so a key is spelled once. Settings written at the top of a file register with RegisterRootKeys,
// whose name labels the section but is not part of any key.
//
// Defaults live in the binary and may change between releases. A written value is never rewritten; an
// absent key follows the running binary's default.
//
// Resolve reduces a node's sources to one value per declared key, in rising precedence: defaults, file,
// environment, flags. It also reports which keys a non-default source supplied and which keys a source
// carried that no section declares. It answers for every declared key or returns an error.
//
// A registration this package cannot use is recorded as a Defect rather than panicking, because
// registration runs during package initialisation and a panic would break every seid invocation.
// A section's own test asserts Defects is empty. spec_test.go holds each registration rule.
//
// Adding a section:
//
//  1. Name it, and use the name as the first segment of every key it declares.
//  2. Register the reader's struct with a per-mode default.
//  3. Assert the registration produced no Defect.
//  4. Test the derived keys against the reader, so a key that reaches nothing fails.
package registry
