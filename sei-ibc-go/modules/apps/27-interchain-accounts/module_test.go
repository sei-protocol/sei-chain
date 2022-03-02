package ica_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	dbm "github.com/tendermint/tm-db"

	ica "github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts"
	controllertypes "github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/controller/types"
	hosttypes "github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/host/types"
	"github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
	"github.com/cosmos/ibc-go/v3/testing/simapp"
)

type InterchainAccountsTestSuite struct {
	suite.Suite

	coordinator *ibctesting.Coordinator
}

func TestICATestSuite(t *testing.T) {
	suite.Run(t, new(InterchainAccountsTestSuite))
}

func (suite *InterchainAccountsTestSuite) SetupTest() {
	suite.coordinator = ibctesting.NewCoordinator(suite.T(), 2)
}

func (suite *InterchainAccountsTestSuite) TestInitModule() {
	app := simapp.NewSimApp(log.NewNopLogger(), dbm.NewMemDB(), nil, true, map[int64]bool{}, simapp.DefaultNodeHome, 5, simapp.MakeTestEncodingConfig(), simapp.EmptyAppOptions{})
	icamodule, ok := app.GetModuleManager().Modules[types.ModuleName].(ica.AppModule)
	suite.Require().True(ok)

	header := tmproto.Header{
		ChainID: "testchain",
		Height:  1,
		Time:    suite.coordinator.CurrentTime.UTC(),
	}

	ctx := app.GetBaseApp().NewContext(true, header)

	// ensure params are not set
	suite.Require().Panics(func() {
		app.ICAControllerKeeper.GetParams(ctx)
	})
	suite.Require().Panics(func() {
		app.ICAHostKeeper.GetParams(ctx)
	})

	controllerParams := controllertypes.DefaultParams()
	controllerParams.ControllerEnabled = true

	hostParams := hosttypes.DefaultParams()
	expAllowMessages := []string{"sdk.Msg"}
	hostParams.HostEnabled = true
	hostParams.AllowMessages = expAllowMessages

	suite.Require().False(app.IBCKeeper.PortKeeper.IsBound(ctx, types.PortID))

	icamodule.InitModule(ctx, controllerParams, hostParams)

	controllerParams = app.ICAControllerKeeper.GetParams(ctx)
	suite.Require().True(controllerParams.ControllerEnabled)

	hostParams = app.ICAHostKeeper.GetParams(ctx)
	suite.Require().True(hostParams.HostEnabled)
	suite.Require().Equal(expAllowMessages, hostParams.AllowMessages)

	suite.Require().True(app.IBCKeeper.PortKeeper.IsBound(ctx, types.PortID))
}
