// Package configmanager selects how seid loads its configuration.
//
// SEI_CONFIG_MANAGER picks the manager. Unset or "legacy" uses the legacy loader unchanged. "v2" reads the
// node's configuration from sei.toml. root.go calls Select once, during PersistentPreRunE.
//
// # sei.toml is the configuration
//
// Under v2, the resolution answers every declared key. A key sei.toml writes takes the written value. A key
// it leaves out takes the value this binary declares for this kind of node. Neither app.toml nor config.toml
// supplies the value of a declared key.
//
// Both are still read. The boot's own handler parses them to build the source the rest of the boot reads,
// and it refuses to start on a file it cannot parse or a value its own rules reject. So a node with a
// complete sei.toml still does not start on a malformed legacy file. What this manager replaces is their
// values for a declared key, not the reading of them.
//
// A sparse sei.toml is therefore a large change to a node whose app.toml was hand-tuned. Every declared key
// the file leaves out moves to its declared value, and there are more than two hundred.
//
// This binary decides which keys are declared. A section reaches the registry by being linked in, so two
// builds can declare different sets. What one binary reports about a file does not describe what another
// does with it.
//
// A node reads a setting in one of two ways, and both are delivered. Most settings are looked up by name
// from a source the boot builds, so a resolved value is installed into that source. The node's own
// configuration file is decoded before any lookup, so a value installed afterwards reaches nothing. Those
// settings are decoded into a copy of the struct and published into it. Each section states which way it
// needs, and the registry records it.
//
// # Refusing nothing
//
// Nothing this manager does stops a node starting. Every failure here leaves each key reading as it always
// did, and the node starts: a missing sei.toml, an unreadable one, an unknown mode, a value that decodes to
// something else, or a panic in the delivery. A mistyped line in this file must not become an outage on the
// next restart. What the legacy files themselves refuse is theirs, and unchanged.
//
// A refusal covers one section, not the file, because a decode is all or nothing for what it is handed. An
// operator who fixes one setting and mistypes another gets the first one.
//
// The cost is that an unusable value is reported rather than refused. These reports are the only signal an
// operator has.
//
// # Asking before a restart
//
// This package also owns `seid config check`. It answers what a node's sei.toml reaches without starting
// the node. It resolves the file the way a boot resolves it. Then it reports every value a delivery would
// refuse, every key no section declares, a disagreement about what kind of node this is, and whether the
// gate would make a boot read the file at all. Its exit status is the answer: anything that would stop the
// node exits non-zero.
//
// It exists because a boot cannot refuse a file. Every failure there is a report on a node that has already
// restarted, and the same questions have exact answers beforehand.
package configmanager
