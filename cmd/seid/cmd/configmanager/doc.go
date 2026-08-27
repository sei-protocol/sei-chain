// Package configmanager selects how seid loads its configuration, behind the SEI_CONFIG_MANAGER gate.
//
// SEI_CONFIG_MANAGER picks the manager: unset or "legacy" uses the legacy loader unchanged, and "v2" reads
// the node's configuration from sei.toml. root.go calls Select once, during PersistentPreRunE.
//
// # sei.toml is the configuration
//
// Under v2, every declared key is answered by the resolution and nothing else. A key sei.toml writes takes
// the written value; a key it leaves out takes the value this binary declares for the kind of node this is.
// app.toml and config.toml are not consulted for a declared key.
//
// So a sparse sei.toml is not a small change to a node whose app.toml was hand-tuned. Every declared key
// the file leaves out moves to its declared value, and there are more than two hundred of them.
//
// Which keys are declared is a property of this binary. A section reaches the registry by being linked in,
// so two builds can answer for different sets of keys, and a question asked of one is not answered for
// another.
//
// A node reads a setting one of two ways and both are delivered. Most are looked up by name from a source
// the boot builds, so a resolved value is installed into it. The settings the node's own configuration file
// carries are decoded out of it before any lookup happens, so a value installed afterwards would reach
// nothing; those are decoded into a copy of the struct and published into it. A section names which of the
// two it needs and the registry answers for the name.
//
// # Refusing nothing
//
// Nothing here can stop a node starting. A missing sei.toml, an unreadable one, a mode this binary does not
// know, a value that decodes to something other than what it says, or a panic in the delivery itself all
// leave every key reading as it always has, and the node starts. A mistyped line in a hand-edited file must
// not become an outage on the next restart.
//
// A refusal is per section, not per file, because a decode is all or nothing for whatever it is handed. An
// operator who fixes one setting and mistypes another gets the first one.
//
// What that costs is that a value which does not arrive is reported rather than refused, which makes these
// reports the only signal an operator has.
package configmanager
