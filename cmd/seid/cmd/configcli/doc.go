// Package configcli holds the sei.toml verbs.
//
// Seven commands over one versioned file. generate writes the whole file for a node mode, and with
// --from-legacy builds it from the node's existing configuration instead of from defaults. show
// prints it, diff compares it against this binary's defaults, and doctor reports every written
// setting this binary does not recognize along with every value it cannot read. set and unset are
// conveniences over hand-editing, and upgrade moves the file through the migration chain.
//
// set converts a value on the way in, so a value typed at a command line is never the wrong type.
// Hand-editing the file is equally legitimate and reaches no such check, which is why doctor reads
// every written value against its key's declared type. Doctor checks types; the enum and range a
// section wants for its own keys need a way to declare them, and StateCommitConfig and
// AutobahnBlockDBConfig already carry Validate methods that are the shape to build on.
//
// A key written in the file is a commitment this binary never rewrites. A key absent from it
// follows the default for the node's mode, which may change between releases, and regenerating
// moves a node onto current defaults. Nothing here runs at boot: only these commands read and
// write the file.
//
// These commands live under the existing config command, which already reads and writes
// client.toml, so every configuration verb sits in one place. Cobra resolves a subcommand
// before it treats an argument as positional, so `config generate` reaches a verb here while
// `config chain-id` still means the client setting it always meant. That holds only while no verb
// is named after a client configuration key, which a test in cmd/seid/cmd compares. They appear
// only where the v2 configuration manager runs, because they act on a file no other manager reads.
//
// Two things an implementer should not build. Every existing operator-facing key keeps its current
// name, so the keys whose unused struct tag would derive a cleaner spelling need no migration:
// renaming ss-keep-recent, ss-enable, ss-prune-interval, evm-ss-split or sc-async-commit-buffer
// transforms a file that is already correct. And an operator writes the experimental table, never
// seid, so set refuses a key in it.
//
// What generate covers is measured rather than assumed. A key reaches a reader as a dotted string
// built at its call site, so reading the tree cannot find the full set. app/observed_config_test.go
// wraps the source an application construction reads from, records every key it asks for, and
// compares that against what generate produces for each declared section. Whatever the construction
// reads outside a declared section is the remaining migration, and that test reports the count so it
// stays visible as it falls.
package configcli
