//go:build mock_chain_validation

package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// Exactly one combination may start: a non-validator pinned to memiavl_only.
func TestAssertReserveNodeAllowed_MockChainValidation_Matrix(t *testing.T) {
	for _, nodeMode := range allNodeModes() {
		for _, writeMode := range allSCWriteModes() {
			allowed := nodeMode != tmcfg.ModeValidator && writeMode == sctypes.MemiavlOnly
			err := assertReserveNodeAllowed(nodeMode, writeMode)
			if allowed {
				require.NoError(t, err, "mode %q with write mode %q must be accepted", nodeMode, writeMode)
				continue
			}
			require.Error(t, err, "mode %q with write mode %q must be refused", nodeMode, writeMode)
		}
	}
}

// Validator mode is refused whatever the write mode, so a correctly pinned
// validator does not slip through.
func TestAssertReserveNodeAllowed_MockChainValidation_RefusesPinnedValidator(t *testing.T) {
	err := assertReserveNodeAllowed(tmcfg.ModeValidator, sctypes.MemiavlOnly)
	require.Error(t, err)
	require.Contains(t, err.Error(), tmcfg.ModeFull)
}

// Auto is what a forgotten sc-write-mode-enable-auto resolves to, and it is the
// failure the write-mode half of this guard exists for, so the message must
// name that key.
func TestAssertReserveNodeAllowed_MockChainValidation_ErrorNamesTheAutoKey(t *testing.T) {
	err := assertReserveNodeAllowed(tmcfg.ModeFull, sctypes.Auto)
	require.Error(t, err)
	require.Contains(t, err.Error(), "sc-write-mode-enable-auto")
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
