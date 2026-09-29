// Package configmanager selects how seid loads its configuration.
//
// SEI_CONFIG_MANAGER picks the manager: unset or "legacy" uses the legacy loader unchanged, and "v2"
// resolves every declared key from sei.toml, taking the binary's per-mode default for a key the file
// leaves out. root.go calls Select once, during PersistentPreRunE.
//
// Under v2, app.toml and config.toml are still parsed by the legacy handler, but no longer supply a
// declared key's value. Values reach looked-up settings by installing into the boot's source, and
// decoded sections (config.toml) by publishing into the decoded struct.
//
// Nothing here stops a node starting. A missing or unreadable sei.toml, an unknown mode, an unusable
// value, or a panic during delivery is reported and leaves the affected keys reading as before. A
// refusal covers one section, not the whole file.
//
// `seid config check` reports, without starting the node, what a boot would refuse, and exits non-zero
// if anything would.
package configmanager
