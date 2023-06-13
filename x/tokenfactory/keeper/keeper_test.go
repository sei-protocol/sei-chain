package keeper_test

import (
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/suite"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/sei-protocol/sei-chain/app/apptesting"
	"github.com/sei-protocol/sei-chain/x/tokenfactory/keeper"
	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

type KeeperTestSuite struct {
	apptesting.KeeperTestHelper

	queryClient     types.QueryClient
	bankQueryClient banktypes.QueryClient
	msgServer       types.MsgServer
	// defaultDenom is on the suite, as it depends on the creator test address.
	defaultDenom string
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (suite *KeeperTestSuite) SetupTest() {
	suite.Setup()

	suite.SetupTokenFactory()

	suite.queryClient = types.NewQueryClient(suite.QueryHelper)
	suite.bankQueryClient = banktypes.NewQueryClient(suite.QueryHelper)
	suite.msgServer = keeper.NewMsgServerImpl(suite.App.TokenFactoryKeeper)
}

func (suite *KeeperTestSuite) CreateDefaultDenom() {
	res, _ := suite.msgServer.CreateDenom(sdk.WrapSDKContext(suite.Ctx), types.NewMsgCreateDenom(suite.TestAccs[0].String(), "bitcoin"))
	suite.defaultDenom = res.GetNewTokenDenom()
}

func (suite *KeeperTestSuite) TestCreateModuleAccount() {
	app := suite.App

	// remove module account
	tokenfactoryModuleAccount := app.AccountKeeper.GetAccount(suite.Ctx, app.AccountKeeper.GetModuleAddress(types.ModuleName))
	app.AccountKeeper.RemoveAccount(suite.Ctx, tokenfactoryModuleAccount)

	// ensure module account was removed
	suite.Ctx = app.BaseApp.NewContext(false, tmproto.Header{})
	tokenfactoryModuleAccount = app.AccountKeeper.GetAccount(suite.Ctx, app.AccountKeeper.GetModuleAddress(types.ModuleName))
	suite.Require().Nil(tokenfactoryModuleAccount)

	// create module account
	app.TokenFactoryKeeper.CreateModuleAccount(suite.Ctx)

	// check that the module account is now initialized
	tokenfactoryModuleAccount = app.AccountKeeper.GetAccount(suite.Ctx, app.AccountKeeper.GetModuleAddress(types.ModuleName))
	suite.Require().NotNil(tokenfactoryModuleAccount)
}
