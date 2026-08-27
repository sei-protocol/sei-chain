// Package configmanager selects how seid loads its configuration, behind the SEI_CONFIG_MANAGER gate.
//
// SEI_CONFIG_MANAGER picks the manager: unset or "legacy" uses the legacy loader unchanged, and "v2" uses
// the one built on the configuration registry. root.go calls Select once, during PersistentPreRunE.
//
// The legacy manager forwards to the legacy interception handler and does nothing else. The v2 manager runs
// that same handler on the operator's own files, so every key a node reads is answered the way it has
// always been answered, and then delivers whatever sei.toml supplies on top of that. It also runs an
// advisory validation pass, which never rewrites a file and never refuses a boot.
//
// # Delivering a value
//
// A node reads a setting one of two ways, and only one of them can be delivered from here today. Most
// settings are looked up by name from a source the boot builds, so a resolved value reaches them by being
// installed into that source. The settings the node's own configuration file carries are read once, by
// decoding that file into a struct before any lookup happens, so a value installed into the source
// afterwards reaches nothing at all. Those sections are identified and deliberately left out of the
// install, because installing a value that changes nothing is worse than not installing it: it reads as
// applied everywhere except in the node.
//
// Only what a source supplied is installed. A resolution answers for every declared key, so installing all
// of it would write a default over whatever an operator's own file holds for every key their sei.toml does
// not mention.
//
// # Refusing nothing
//
// Nothing here can stop a node starting. A missing sei.toml, an unreadable one, a mode this binary does not
// know, a value the install refuses, or a panic in the delivery itself all leave every key reading as it
// always has, and the node starts. Selecting this manager is a switch rather than a configuration change,
// and a mistyped line in a hand-edited file must not become an outage on the next restart.
//
// What that costs is that a value which does not arrive is reported rather than refused, which makes these
// reports the only signal an operator has. So they are held at a level that survives a fleet running its
// nodes quiet, they name the source they are about, and they are bounded, because a report that fires on
// every boot is one nobody reads.
//
// Deferred: a path that writes sei.toml, so a node's configuration can be rendered from it rather than only
// read into it.
package configmanager
