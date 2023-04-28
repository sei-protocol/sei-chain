package app_test

import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/upgrade/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestUpgradesListIsSorted(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := app.NewTestWrapper(t, tm, valPub)
	testWrapper.App.RegisterUpgradeHandlers()
}

// Test community tax param is set to 0 as part of upgrade 1.2.3beta
func TestDistributionCommunityTaxParamMigration(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := app.NewTestWrapper(t, tm, valPub)
	testWrapper.App.RegisterUpgradeHandlers()
	params := testWrapper.App.DistrKeeper.GetParams(testWrapper.Ctx)
	testWrapper.Require().Equal(params.CommunityTax, sdk.NewDec(0))
}

func TestSkipOptimisticProcessingOnUpgrade(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := app.NewTestWrapper(t, tm, valPub)

	testCtx := testWrapper.App.BaseApp.NewContext(false, tmproto.Header{Height: 1, ChainID: "sei-test", Time: tm})
	testWrapper.App.UpgradeKeeper.ScheduleUpgrade(testWrapper.Ctx, types.Plan{
		Name:   "test-plan",
		Height: 4,
	})
	res, _ := testWrapper.App.ProcessProposalHandler(testCtx.WithBlockHeight(4), &abci.RequestProcessProposal{
		Height: 1,
	})
	require.Equal(t, res.Status, abci.ResponseProcessProposal_ACCEPT)
	require.True(t, testWrapper.App.GetOptimisticProcessingInfo().Aborted)

	testWrapper.App.ClearOptimisticProcessingInfo()
	testWrapper.App.UpgradeKeeper.ScheduleUpgrade(testWrapper.Ctx, types.Plan{
		Name:   "test-plan",
		Height: 5,
	})
	res, _ = testWrapper.App.ProcessProposalHandler(testCtx.WithBlockHeight(4), &abci.RequestProcessProposal{
		Height: 1,
	})

	// Wait for completion signal before proceeding
	<- testWrapper.App.GetOptimisticProcessingInfo().Completion

	require.Equal(t, res.Status, abci.ResponseProcessProposal_ACCEPT)
	require.False(t, testWrapper.App.GetOptimisticProcessingInfo().Aborted)
}
