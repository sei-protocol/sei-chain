//go:build !mock_chain_validation

package server

import sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"

// assertReserveNodeAllowed reports whether a node may start with the given
// Tendermint mode and effective state-commit write mode. Production builds
// accept every combination the configuration itself accepts.
// mock_block_validation is not a reserve build, so this guard does not define
// its validator policy.
func assertReserveNodeAllowed(string, sctypes.WriteMode) error { return nil }
