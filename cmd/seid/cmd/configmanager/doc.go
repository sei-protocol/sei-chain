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
// A setting whose reader decodes the node's own configuration file, rather than looking the key up, is
// resolved and reported here but not delivered: a value installed into the source after that decode reaches
// nothing at all. Those keys read as they always have.
//
// # Refusing nothing
//
// Nothing here can stop a node starting. A missing sei.toml, an unreadable one, a mode this binary does not
// know, a value the install refuses, or a panic in the delivery itself all leave every key reading as it
// always has, and the node starts. A mistyped line in a hand-edited file must not become an outage on the
// next restart. What that costs is that a value which does not arrive is reported rather than refused,
// which makes these reports the only signal an operator has.
package configmanager
