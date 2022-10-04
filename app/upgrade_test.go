package app_test

import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/app"
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
