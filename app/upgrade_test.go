package app_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	storekeys "github.com/sei-protocol/sei-chain/sei-db/common/keys"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/upgradetest"
	"github.com/stretchr/testify/require"
)

func TestUpgradesListIsSorted(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := app.NewTestWrapper(t, tm, valPub, false)
	testWrapper.App.RegisterUpgradeHandlers()
}

func TestUpgradePlanNamesMatchRegisteredHandlers(t *testing.T) {
	shipped := app.ReleaseUpgrades()
	require.NotEmpty(t, shipped)
	t.Setenv("UPGRADE_VERSION_LIST", strings.Join(shipped, "\n"))

	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := app.NewTestWrapper(t, tm, valPub, false)
	testWrapper.App.RegisterUpgradeHandlers()

	for _, name := range shipped {
		require.True(t, testWrapper.App.UpgradeKeeper.HasHandler(name),
			"no handler registered for shipped upgrade %q", name)
	}

	boundary, err := upgradetest.Current()
	require.NoError(t, err)
	require.Contains(t, shipped, boundary.To,
		"go run ./upgradetest/cmd/boundary to prints %q, which is not a shipped upgrade", boundary.To)

	names := []string{app.LatestUpgrade}
	if boundary.To != app.LatestUpgrade {
		names = append(names, boundary.To)
	}
	for _, name := range names {
		for _, miss := range upgradeNameNearMisses(name) {
			require.NotEqual(t, name, miss)
			require.False(t, testWrapper.App.UpgradeKeeper.HasHandler(miss),
				"handler registered for near-miss name %q of %q", miss, name)

			require.NoError(t, testWrapper.App.UpgradeKeeper.ScheduleUpgrade(testWrapper.Ctx, types.Plan{
				Name:   miss,
				Height: testWrapper.Ctx.BlockHeight(),
			}))
			var panicked any
			func() {
				defer func() { panicked = recover() }()
				upgrade.BeginBlocker(testWrapper.App.UpgradeKeeper, testWrapper.Ctx)
			}()
			requireUpgradeNeededPanic(t, panicked, miss, testWrapper.Ctx.BlockHeight())
			require.Zero(t, testWrapper.App.UpgradeKeeper.GetDoneHeight(testWrapper.Ctx, name),
				"near-miss plan %q applied the handler for %q", miss, name)
			require.Zero(t, testWrapper.App.UpgradeKeeper.GetDoneHeight(testWrapper.Ctx, miss),
				"near-miss plan %q was applied", miss)
			plan, found := testWrapper.App.UpgradeKeeper.GetUpgradePlan(testWrapper.Ctx)
			require.True(t, found, "near-miss plan %q was cleared instead of halting", miss)
			require.Equal(t, miss, plan.Name)
		}
	}
}

// upgradeNameNearMisses returns plausible misspellings of an upgrade plan name.
func upgradeNameNearMisses(name string) []string {
	return []string{
		name + ".0",
		strings.ToUpper(name),
		" " + name + " ",
	}
}

func requireUpgradeNeededPanic(t *testing.T, panicked any, name string, height int64) {
	t.Helper()
	require.NotNil(t, panicked, "plan %q was produced without a handler", name)
	msg := fmt.Sprint(panicked)
	require.Contains(t, msg, fmt.Sprintf(`UPGRADE "%s" NEEDED`, name),
		"halt panic is missing the upgrade name: %v", panicked)
	require.Contains(t, msg, fmt.Sprintf("height: %d", height),
		"halt panic is missing plan height %d: %v", height, panicked)
}

// Test community tax param is set to 0 as part of upgrade 1.2.3beta
func TestDistributionCommunityTaxParamMigration(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := app.NewTestWrapper(t, tm, valPub, false)
	testWrapper.App.RegisterUpgradeHandlers()
	params := testWrapper.App.DistrKeeper.GetParams(testWrapper.Ctx)
	testWrapper.Require().Equal(params.CommunityTax, sdk.NewDec(0))
}

func TestV67RemovesRetiredModuleVersions(t *testing.T) {
	t.Setenv("UPGRADE_VERSION_LIST", "v6.7")
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := app.NewTestWrapper(t, tm, valPub, false)
	testWrapper.App.RegisterUpgradeHandlers()

	versionMap := testWrapper.App.UpgradeKeeper.GetModuleVersionMap(testWrapper.Ctx)
	versionMap[storekeys.IBCStoreKey] = 1
	versionMap["capability"] = 1
	versionMap["feegrant"] = 1
	versionMap["transfer"] = 2
	testWrapper.App.UpgradeKeeper.SetModuleVersionMap(testWrapper.Ctx, versionMap)

	testWrapper.App.UpgradeKeeper.ApplyUpgrade(testWrapper.Ctx, types.Plan{
		Name:   "v6.7",
		Height: testWrapper.Ctx.BlockHeight(),
	})

	versionMap = testWrapper.App.UpgradeKeeper.GetModuleVersionMap(testWrapper.Ctx)
	require.NotContains(t, versionMap, storekeys.IBCStoreKey)
	require.NotContains(t, versionMap, "capability")
	require.NotContains(t, versionMap, "feegrant")
	require.NotContains(t, versionMap, "transfer")
}

func TestSkipOptimisticProcessingOnUpgrade(t *testing.T) {
	t.Parallel()

	t.Run("Test optimistic processing is skipped on upgrade", func(t *testing.T) {
		tm := time.Now().UTC()
		valPub := secp256k1.GenPrivKey().PubKey()
		testWrapper := app.NewTestWrapper(t, tm, valPub, false)

		// No optimistic processing with upgrade scheduled
		testCtx := testWrapper.App.BaseApp.NewContext(false, tmproto.Header{Height: 3, ChainID: "sei-test", Time: tm})

		testWrapper.App.UpgradeKeeper.ScheduleUpgrade(testWrapper.Ctx, types.Plan{
			Name:   "test-plan",
			Height: testCtx.BlockHeight(),
		})
		plan, found := testWrapper.App.UpgradeKeeper.GetUpgradePlan(testCtx)
		require.True(t, found)
		require.True(t, plan.ShouldExecute(testCtx))

		res, _ := testWrapper.App.ProcessProposalHandler(testCtx, &abci.RequestProcessProposal{
			Header: &tmproto.Header{Height: 1, ChainID: "sei-test"},
		})
		require.Equal(t, res.Status, abci.ResponseProcessProposal_ACCEPT)
		require.True(t, testWrapper.App.GetOptimisticProcessingInfo().Aborted)
	})

	t.Run("Test optimistic processing if no upgrade", func(t *testing.T) {
		tm := time.Now().UTC()
		valPub := secp256k1.GenPrivKey().PubKey()
		testWrapper := app.NewTestWrapper(t, tm, valPub, false)
		testCtx := testWrapper.App.BaseApp.NewContext(false, tmproto.Header{Height: 3, ChainID: "sei-test", Time: tm})

		testWrapper.App.UpgradeKeeper.ScheduleUpgrade(testWrapper.Ctx, types.Plan{
			Name:   "test-plan",
			Height: testCtx.BlockHeight() + 1,
		})
		plan, found := testWrapper.App.UpgradeKeeper.GetUpgradePlan(testCtx)
		require.True(t, found)
		require.False(t, plan.ShouldExecute(testCtx))

		go func() {
			testWrapper.App.ProcessProposalHandler(testCtx, &abci.RequestProcessProposal{Header: &tmproto.Header{Height: 1, ChainID: "sei-test"}})
		}()

		require.Eventually(t, func() bool {
			opi := testWrapper.App.GetOptimisticProcessingInfo()
			if opi.Completion == nil {
				return false
			}
			<-opi.Completion
			return true
		}, 5*time.Second, time.Millisecond*100)

		// require.Equal(t, res.Status, abci.ResponseProcessProposal_ACCEPT)
		require.False(t, testWrapper.App.GetOptimisticProcessingInfo().Aborted)
	})
}
