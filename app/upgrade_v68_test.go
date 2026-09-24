//go:build upgrade_v68

package app_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/sei-protocol/sei-chain/testutil/processblock/msgs"
	"github.com/sei-protocol/sei-chain/upgradetest"
	"github.com/stretchr/testify/require"
)

const v68UpgradeName = "v6.8"

func newV68Chain(t *testing.T) *processblock.App {
	t.Helper()
	t.Setenv("UPGRADE_VERSION_LIST", v68UpgradeName)
	app := processblock.NewTestApp(t)
	processblock.CommonPreset(app)
	app.RegisterUpgradeHandlers()
	return app
}

func applyV68(t *testing.T, app *processblock.App) {
	t.Helper()
	app.UpgradeKeeper.ApplyUpgrade(app.Ctx(), upgradetypes.Plan{
		Name:   v68UpgradeName,
		Height: app.Ctx().BlockHeight(),
	})
}

func TestV68Upgrade(t *testing.T) {
	app := newV68Chain(t)
	beforeVersions := app.UpgradeKeeper.GetModuleVersionMap(app.Ctx())
	require.Contains(t, beforeVersions, "oracle")
	sender := app.NewSignableAccount("v68-sender")
	receiver := app.NewAccount()
	app.FundAccount(sender, 1_000_000)
	require.Equal(t, []uint32{0}, app.RunBlock([]signing.Tx{
		app.Sign(sender, 0, msgs.Send(sender, receiver, 1)),
	}))

	applyV68(t, app)
	require.Equal(t, beforeVersions, app.UpgradeKeeper.GetModuleVersionMap(app.Ctx()))
	require.Equal(t, []uint32{0}, app.RunBlock([]signing.Tx{
		app.Sign(sender, 0, msgs.Send(sender, receiver, 1)),
	}))
}

func TestV68UnupgradedBinaryHaltsAtPlanHeight(t *testing.T) {
	t.Setenv("UPGRADE_VERSION_LIST", "v6.7")
	app := processblock.NewTestApp(t)
	processblock.CommonPreset(app)
	app.RegisterUpgradeHandlers()
	require.False(t, app.UpgradeKeeper.HasHandler(v68UpgradeName))
	require.NoError(t, app.UpgradeKeeper.ScheduleUpgrade(app.Ctx(), upgradetypes.Plan{
		Name: v68UpgradeName, Height: 3,
	}))
	app.RunBlock(nil)
	app.RunBlock(nil)
	require.Panics(t, func() { app.RunBlock(nil) })
}

func TestV68CrossVersion(t *testing.T) {
	upgradetest.RunCrossVersion(t,
		func(t *testing.T, chain *upgradetest.CrossVersion) {
			require.Equal(t, v68UpgradeName, chain.UpgradeName(t))
			chain.Record(t, "module-versions", chain.ModuleVersions(t))
			chain.RequireDeliverTxSuccess(t, "v6.7 bank send", chain.Seid(
				"12345678\n",
				"tx", "bank", "send", "admin", "node_admin", "1usei",
				"--from", "admin", "--chain-id", "sei", "--fees", "200000usei",
				"--gas", "2000000", "--broadcast-mode", "sync", "--yes", "--output", "json",
			))
		},
		func(t *testing.T, chain *upgradetest.CrossVersion) {
			var before []string
			chain.Replay(t, "module-versions", &before)
			require.Equal(t, before, chain.ModuleVersions(t))
			chain.RequireDeliverTxSuccess(t, "v6.8 bank send", chain.Seid(
				"12345678\n",
				"tx", "bank", "send", "admin", "node_admin", "1usei",
				"--from", "admin", "--chain-id", "sei", "--fees", "200000usei",
				"--gas", "2000000", "--broadcast-mode", "sync", "--yes", "--output", "json",
			))
		},
	)
}
