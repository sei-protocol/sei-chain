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
func assertReserveNodeAllowed(nodeMode string, writeMode sctypes.WriteMode) error {
	// This build swallows app-hash and validator-set validation failures, so it
	// cannot safely vote when its state diverges from the proposed block.
	if nodeMode == tmcfg.ModeValidator {
		return fmt.Errorf(
			"mock_chain_validation builds must not run as a validator: this build "+
				"swallows app-hash and validator-set validation failures, so it cannot "+
				"safely vote; set mode = %q in config.toml",
			tmcfg.ModeFull,
		)
	}
	// An unpinned node joins a governance-driven migration and stops preserving
	// the memIAVL state the reserve role exists to retain.
	if writeMode != sctypes.MemiavlOnly {
		return fmt.Errorf(
			"mock_chain_validation builds must run %[1]q, got %[2]q: set "+
				"state-commit.sc-write-mode = %[1]q and "+
				"state-commit.sc-write-mode-enable-auto = false in app.toml",
			sctypes.MemiavlOnly, writeMode,
		)
	}
	return nil
}
