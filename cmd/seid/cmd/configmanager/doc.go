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
// That is the point of v2 rather than a side effect of it. One file states what a node runs, and what it
// does not state is answered by a declaration living in the binary, so two nodes with the same sei.toml run
// the same configuration whatever is left on their disks.
//
// The legacy handler still runs first, because it builds the source the rest of the boot reads from, creates
// the home directory and applies the log level. What it read out of app.toml and config.toml for a declared
// key is then replaced. A key no section declares is left as that handler resolved it, and that remainder is
// the migration still to do.
//
// # What this costs, and what has to exist first
//
// On a node whose app.toml was hand-tuned, every declared key absent from sei.toml moves to its declared
// value, and there are more than two hundred and fifty declared keys. A sparse sei.toml is therefore not a
// small change to such a node; it is most of its configuration.
//
// So a path that renders sei.toml from a node's existing files has to land before this is switched on
// anywhere, and seid init has to write one for a new node. Neither exists yet, which is why the gate
// defaults to the legacy manager.
//
// # Delivering a value
//
// A node reads a setting one of two ways, and only one of them can be delivered from here today. Most
// settings are looked up by name from a source the boot builds, so a resolved value reaches them by being
// installed into that source. The settings the node's own configuration file carries are read once, by
// decoding that file into a struct before any lookup happens, so a value installed into the source
// afterwards reaches nothing at all. Those sections are identified and deliberately left out of the
// install, because installing a value that changes nothing is worse than not installing it: it reads as
// applied everywhere except in the node. They are reported instead, and delivered by the change after this
// one.
//
// # Refusing nothing
//
// Nothing here can stop a node starting. A missing sei.toml, an unreadable one, a mode this binary does not
// know, a value the install refuses, or a panic in the delivery itself all leave every key reading as it
// always has, and the node starts. A mistyped line in a hand-edited file must not become an outage on the
// next restart.
//
// What that costs is that a value which does not arrive is reported rather than refused, which makes these
// reports the only signal an operator has. So they are held at a floor that survives a fleet running its
// nodes quiet, without lowering a level an operator raised; they name the source they are about; and they
// are bounded, because a report that fires on every boot is one nobody reads.
//
// Deferred: a path that writes sei.toml, so a node's configuration can be rendered from its existing files
// rather than only read out of a file somebody has to author by hand.
package configmanager
