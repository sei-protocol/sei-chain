// Package configmanager selects how seid loads its configuration, behind the
// SEI_CONFIG_MANAGER gate. Every manager boots the node identically; the only
// variable is an advisory validation pass that never rewrites a file and never
// refuses boot.
//
// SEI_CONFIG_MANAGER picks the manager: unset or "legacy" uses the legacy
// loader unchanged; "v2" uses the sei-config-backed manager. root.go calls
// Select once, during PersistentPreRunE.
//
// Both managers boot from the same two channels — serverCtx.Config and
// serverCtx.Viper — and v2 populates them exactly as legacy does: it re-enters
// the legacy reader on the operator's own files instead of rewriting them. Its
// validation pass is advisory because sei-config's read fidelity is still being
// hardened, and a gap in the model must not fail a valid node.
//
// Two things are deferred: making validation fatal, and authoring a canonical
// sei.toml to render the legacy files from (the generate path). See PLT-775 and
// the design (bdchatham-designs designs/config-manager/DESIGN.md).
package configmanager
