package keeper_test

import (
	icatypes "github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
)

func (suite *KeeperTestSuite) TestRegisterInterchainAccount() {
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
			"MsgChanOpenInit fails - channel is already active & in state OPEN",
			func() {
				portID, err := icatypes.NewControllerPortID(TestOwnerAddress)
				suite.Require().NoError(err)

				suite.chainA.GetSimApp().ICAControllerKeeper.SetActiveChannelID(suite.chainA.GetContext(), portID, path.EndpointA.ChannelID)

				counterparty := channeltypes.NewCounterparty(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
				channel := channeltypes.Channel{
					State:          channeltypes.OPEN,
					Ordering:       channeltypes.ORDERED,
					Counterparty:   counterparty,
					ConnectionHops: []string{path.EndpointA.ConnectionID},
					Version:        TestVersion,
				}
				suite.chainA.GetSimApp().IBCKeeper.ChannelKeeper.SetChannel(suite.chainA.GetContext(), portID, path.EndpointA.ChannelID, channel)
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

			err = suite.chainA.GetSimApp().ICAControllerKeeper.RegisterInterchainAccount(suite.chainA.GetContext(), path.EndpointA.ConnectionID, owner)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}

		})
	}
}
