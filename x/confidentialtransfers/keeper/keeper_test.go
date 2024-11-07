package keeper_test

import (
	"github.com/sei-protocol/sei-chain/app/apptesting"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/keeper"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/stretchr/testify/suite"
	"testing"
)

type KeeperTestSuite struct {
	apptesting.KeeperTestHelper

	queryClient types.QueryClient
	msgServer   types.MsgServer
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
	suite.App.ConfidentialTransfersKeeper = keeper.NewKeeper(
		suite.App.AppCodec(),
		suite.App.GetKey(types.StoreKey),
		suite.App.GetSubspace(types.ModuleName),
		suite.App.AccountKeeper)
	suite.msgServer = keeper.NewMsgServerImpl(suite.App.ConfidentialTransfersKeeper)
}
