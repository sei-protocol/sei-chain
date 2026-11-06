package evmonlyapp

import (
	"testing"

	"github.com/sei-protocol/sei-chain/app/upgradenames"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/upgrade"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// testAppliedUpgrades are the upgrade names test chains start past.
var testAppliedUpgrades = upgradenames.Release()

func TestEVMOnlyApplicationRecordsAppliedUpgradesAtTheFirstBlock(t *testing.T) {
	app := newGovEVMOnlyTestApp(t, gov.DefaultParams(), 0)
	out, err := precompileCall(t, app, upgrade.Address, upgrade.ABI, upgrade.AppliedPlanMethod, upgradenames.Latest())
	require.NoError(t, err)
	require.Equal(t, uint64(0), out[0].(uint64))

	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(1))
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(2))
	for _, name := range testAppliedUpgrades {
		out, err := precompileCall(t, app, upgrade.Address, upgrade.ABI, upgrade.AppliedPlanMethod, name)
		require.NoError(t, err)
		require.Equal(t, uint64(1), out[0].(uint64), name)
	}
	out, err = precompileCall(t, app, upgrade.Address, upgrade.ABI, upgrade.CurrentPlanMethod)
	require.NoError(t, err)
	require.Zero(t, out[0].(struct {
		Name   string `json:"name"`
		Height uint64 `json:"height"`
		Info   string `json:"info"`
	}).Height)
}

func TestUpgradeHandlersCoverEveryReleaseName(t *testing.T) {
	handlers := upgradeHandlers()
	require.Equal(t, len(upgradenames.Release()), len(handlers))
	for _, name := range upgradenames.Release() {
		_, ok := handlers[name]
		require.True(t, ok, name)
	}
}
