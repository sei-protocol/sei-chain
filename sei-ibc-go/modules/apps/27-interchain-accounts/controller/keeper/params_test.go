package keeper_test

import "github.com/cosmos/ibc-go/v2/modules/apps/27-interchain-accounts/controller/types"

func (suite *KeeperTestSuite) TestParams() {
	expParams := types.DefaultParams()

	params := suite.chainA.GetSimApp().ICAControllerKeeper.GetParams(suite.chainA.GetContext())
	suite.Require().Equal(expParams, params)

	expParams.ControllerEnabled = false
	suite.chainA.GetSimApp().ICAControllerKeeper.SetParams(suite.chainA.GetContext(), expParams)
	params = suite.chainA.GetSimApp().ICAControllerKeeper.GetParams(suite.chainA.GetContext())
	suite.Require().Equal(expParams, params)
}
