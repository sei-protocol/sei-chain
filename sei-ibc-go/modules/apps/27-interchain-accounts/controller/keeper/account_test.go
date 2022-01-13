package keeper_test

import (
	icatypes "github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
)

func (suite *KeeperTestSuite) TestInitInterchainAccount() {
	var (
		owner string
		path  *ibctesting.Path
		err   error
	)

	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{
			"success", func() {}, true,
		},
		{
			"port is already bound",
			func() {
				suite.chainA.GetSimApp().IBCKeeper.PortKeeper.BindPort(suite.chainA.GetContext(), TestPortID)
			},
			false,
		},
		{
			"fails to generate port-id",
			func() {
				owner = ""
			},
			false,
		},
		{
			"MsgChanOpenInit fails - channel is already active",
			func() {
				portID, err := icatypes.NewControllerPortID(TestOwnerAddress)
				suite.Require().NoError(err)

				suite.chainA.GetSimApp().ICAControllerKeeper.SetActiveChannelID(suite.chainA.GetContext(), portID, path.EndpointA.ChannelID)
			},
			false,
		},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest()

			owner = TestOwnerAddress // must be explicitly changed

			path = NewICAPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)

			tc.malleate() // malleate mutates test data

			err = suite.chainA.GetSimApp().ICAControllerKeeper.InitInterchainAccount(suite.chainA.GetContext(), path.EndpointA.ConnectionID, owner)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}

		})
	}
}
