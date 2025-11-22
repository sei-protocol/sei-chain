package keeper_test

import "github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"

func (suite *KeeperTestSuite) TestParams() {
	expParams := types.DefaultParams()

	params := suite.chainA.GetSimApp().TransferKeeper.GetParams(suite.chainA.GetContext())
	suite.Require().Equal(expParams, params)

	expParams.SendEnabled = false
	suite.chainA.GetSimApp().TransferKeeper.SetParams(suite.chainA.GetContext(), expParams)
	params = suite.chainA.GetSimApp().TransferKeeper.GetParams(suite.chainA.GetContext())
	suite.Require().Equal(expParams, params)
}
