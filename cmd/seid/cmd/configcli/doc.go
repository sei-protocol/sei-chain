// Package configcli is the seid config command tree.
//
// Six verbs over one versioned file: generate writes the resolved sei.toml for the declared
// mode, set and unset are conveniences over hand-editing, doctor checks every written key
// against the binary's current definition, upgrade runs the frozen migration chain, and diff
// compares the file against the binary's baselines.
//
// Boot never writes the file. A key present in it is authoritative, a key absent resolves to
// the running binary's baseline, and regenerating is how a node re-baselines.
//
// spec_test.go states what each verb must do, one test per rule, and every test fails until the
// verb exists. Those tests sit behind the configspec build tag so an unbuilt package does not
// fail CI; removing the tag is what declares this package finished.
package configcli
