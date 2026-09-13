//go:build mock_chain_validation

package config

// applyReserveDefaults returns the state-commit defaults for this build's node
// role. This build starts only as a reserve, so it honors the explicit write
// mode rather than resolving to auto.
func applyReserveDefaults(cfg StateCommitConfig) StateCommitConfig {
	// sc-write-mode-enable-auto is never rendered into app.toml, so this in-code
	// default is what a node resolves against unless an operator adds the key by
	// hand. At true the explicit sc-write-mode is discarded and the node joins a
	// governance-driven migration on the first block after the batch size rises,
	// which spends the reserve with nothing to signal that it happened. Flipping
	// it here makes a reserve correct with no operator configuration at all; an
	// explicit key in app.toml still wins, and assertReserveNodeAllowed refuses
	// the node when it wins in the wrong direction.
	cfg.WriteModeEnableAuto = false
	return cfg
}

// reserveStateCommitConfigTemplate is the app.toml fragment this build adds to
// the [state-commit] section. It renders sc-write-mode-enable-auto, which the
// stock template omits.
const reserveStateCommitConfigTemplate = `
# sc-write-mode-enable-auto is rendered by this build and omitted by the stock
# one, because the two default it differently: false here, true there. At false
# the explicit sc-write-mode above is honored, which is what makes this node a
# reserve. Set it to true and this build refuses to start, since an unpinned
# node joins the migration and stops being a recovery source.
sc-write-mode-enable-auto = {{ .StateCommit.WriteModeEnableAuto }}
`
