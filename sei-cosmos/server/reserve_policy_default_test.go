//go:build !mock_chain_validation

package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// A production build must not refuse any mode/write-mode combination; the
// reserve guard exists only in the mock_chain_validation build.
func TestAssertReserveNodeAllowed_Default_AcceptsEveryCombination(t *testing.T) {
	for _, nodeMode := range allNodeModes() {
		for _, writeMode := range allSCWriteModes() {
			require.NoError(t, assertReserveNodeAllowed(nodeMode, writeMode),
				"mode %q with write mode %q must be accepted by a production build", nodeMode, writeMode)
		}
	}
}

func allNodeModes() []string {
	return []string{tmcfg.ModeFull, tmcfg.ModeValidator, tmcfg.ModeSeed}
}

func allSCWriteModes() []sctypes.WriteMode {
	return []sctypes.WriteMode{
		sctypes.MemiavlOnly,
		sctypes.MigrateEVM,
		sctypes.EVMMigrated,
		sctypes.MigrateAllButBank,
		sctypes.AllMigratedButBank,
		sctypes.MigrateBank,
		sctypes.FlatKVOnly,
		sctypes.TestOnlyDualWrite,
		sctypes.Auto,
	}
}
