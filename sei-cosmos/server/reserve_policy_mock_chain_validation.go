//go:build mock_chain_validation

package server

import (
	"fmt"

	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// assertReserveNodeAllowed reports whether a node may start with the given
// Tendermint mode and effective state-commit write mode. This build starts
// only as a non-validator pinned to memiavl_only.
//
// Both conditions are what make the build a reserve rather than a liability.
// The consensus policy compiled in here swallows ErrAppHash and the
// validator-set sentinels, so a validator running it would prevote and
// precommit blocks whose app hash contradicts its own state instead of
// prevoting nil. And a node that is not pinned joins the migration on the
// first block after governance raises the batch size, which spends the reserve
// with nothing to signal that it happened.
//
// Refusing here rather than trusting app.toml costs a restart to discover and
// saves finding out at the first divergent block.
func assertReserveNodeAllowed(nodeMode string, writeMode sctypes.WriteMode) error {
	if nodeMode == tmcfg.ModeValidator {
		return fmt.Errorf(
			"mock_chain_validation builds must not run as a validator: this build "+
				"swallows app-hash and validator-set validation failures, so it cannot "+
				"safely vote; set mode = %q in config.toml",
			tmcfg.ModeFull,
		)
	}
	if writeMode != sctypes.MemiavlOnly {
		return fmt.Errorf(
			"mock_chain_validation builds must run %[1]q, got %[2]q: set "+
				"state-commit.sc-write-mode = %[1]q and "+
				"state-commit.sc-write-mode-enable-auto = false in app.toml (the latter "+
				"is absent from a generated app.toml and defaults to true, which "+
				"discards the former)",
			sctypes.MemiavlOnly, writeMode,
		)
	}
	return nil
}
