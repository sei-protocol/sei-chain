//go:build !mock_chain_validation

package config

// applyReserveDefaults returns the state-commit defaults for this build's node
// role. Production builds have no reserve role and use the defaults unchanged.
func applyReserveDefaults(cfg StateCommitConfig) StateCommitConfig { return cfg }

// reserveStateCommitConfigTemplate is the app.toml fragment this build adds to
// the [state-commit] section. Production builds add nothing.
const reserveStateCommitConfigTemplate = ""
