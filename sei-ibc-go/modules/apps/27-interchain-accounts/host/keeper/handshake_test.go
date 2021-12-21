package keeper_test

import (
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"

	icatypes "github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v3/modules/core/24-host"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
)

func (suite *KeeperTestSuite) TestOnChanOpenTry() {
	var (
		channel             *channeltypes.Channel
		path                *ibctesting.Path
		chanCap             *capabilitytypes.Capability
		counterpartyVersion string
	)

	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{

		{
			"success",
			func() {
				path.EndpointB.SetChannel(*channel)
			},
			true,
		},
		{
			"invalid order - UNORDERED",
			func() {
				channel.Ordering = channeltypes.UNORDERED
			},
			false,
		},
		{
			"invalid port",
			func() {
				path.EndpointB.ChannelConfig.PortID = "invalid-port-id"
			},
			false,
		},
		{
			"invalid counterparty port",
			func() {
				channel.Counterparty.PortId = "invalid-port-id"
			},
			false,
		},
		{
			"connection not found",
			func() {
				channel.ConnectionHops = []string{"invalid-connnection-id"}
				path.EndpointB.SetChannel(*channel)
			},
			false,
		},
		{
			"invalid connection sequence",
			func() {
				portID, err := icatypes.GeneratePortID(TestOwnerAddress, "connection-0", "connection-1")
				suite.Require().NoError(err)

				channel.Counterparty.PortId = portID
				path.EndpointB.SetChannel(*channel)
			},
			false,
		},
		{
			"invalid counterparty connection sequence",
			func() {
				portID, err := icatypes.GeneratePortID(TestOwnerAddress, "connection-1", "connection-0")
				suite.Require().NoError(err)

				channel.Counterparty.PortId = portID
				path.EndpointB.SetChannel(*channel)
			},
			false,
		},
		{
			"invalid version",
			func() {
				channel.Version = "version"
				path.EndpointB.SetChannel(*channel)
			},
			false,
		},
		{
			"invalid counterparty version",
			func() {
				counterpartyVersion = "version"
				path.EndpointB.SetChannel(*channel)
			},
			false,
		},
		{
			"capability already claimed",
			func() {
				path.EndpointB.SetChannel(*channel)
				err := suite.chainB.GetSimApp().ScopedICAHostKeeper.ClaimCapability(suite.chainB.GetContext(), chanCap, host.ChannelCapabilityPath(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID))
				suite.Require().NoError(err)
			},
			false,
		},
		{
			"invalid account address",
			func() {
				portID, err := icatypes.GeneratePortID("invalid-owner-addr", "connection-0", "connection-0")
				suite.Require().NoError(err)

				channel.Counterparty.PortId = portID
				path.EndpointB.SetChannel(*channel)
			},
			false,
		},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			path = NewICAPath(suite.chainA, suite.chainB)
			counterpartyVersion = icatypes.VersionPrefix
			suite.coordinator.SetupConnections(path)

			err := InitInterchainAccount(path.EndpointA, TestOwnerAddress)
			suite.Require().NoError(err)

			// set the channel id on host
			channelSequence := path.EndpointB.Chain.App.GetIBCKeeper().ChannelKeeper.GetNextChannelSequence(path.EndpointB.Chain.GetContext())
			path.EndpointB.ChannelID = channeltypes.FormatChannelIdentifier(channelSequence)

			// default values
			counterparty := channeltypes.NewCounterparty(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			channel = &channeltypes.Channel{
				State:          channeltypes.TRYOPEN,
				Ordering:       channeltypes.ORDERED,
				Counterparty:   counterparty,
				ConnectionHops: []string{path.EndpointB.ConnectionID},
				Version:        TestVersion,
			}

			chanCap, err = suite.chainB.App.GetScopedIBCKeeper().NewCapability(suite.chainB.GetContext(), host.ChannelCapabilityPath(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID))
			suite.Require().NoError(err)

			tc.malleate() // malleate mutates test data

			err = suite.chainB.GetSimApp().ICAHostKeeper.OnChanOpenTry(suite.chainB.GetContext(), channel.Ordering, channel.GetConnectionHops(),
				path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, chanCap, channel.Counterparty, channel.GetVersion(),
				counterpartyVersion,
			)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestOnChanOpenConfirm() {
	var (
		path *ibctesting.Path
	)

	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{

		{
			"success", func() {}, true,
		},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			path = NewICAPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)

			err := InitInterchainAccount(path.EndpointA, TestOwnerAddress)
			suite.Require().NoError(err)

			err = path.EndpointB.ChanOpenTry()
			suite.Require().NoError(err)

			err = path.EndpointA.ChanOpenAck()
			suite.Require().NoError(err)

			tc.malleate() // malleate mutates test data

			err = suite.chainB.GetSimApp().ICAHostKeeper.OnChanOpenConfirm(suite.chainB.GetContext(),
				path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}

		})
	}
}

func (suite *KeeperTestSuite) TestOnChanCloseConfirm() {
	var (
		path *ibctesting.Path
	)

	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{

		{
			"success", func() {}, true,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			path = NewICAPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)

			err := SetupICAPath(path, TestOwnerAddress)
			suite.Require().NoError(err)

			tc.malleate() // malleate mutates test data

			err = suite.chainB.GetSimApp().ICAHostKeeper.OnChanCloseConfirm(suite.chainB.GetContext(),
				path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)

			activeChannelID, found := suite.chainB.GetSimApp().ICAHostKeeper.GetActiveChannelID(suite.chainB.GetContext(), path.EndpointB.ChannelConfig.PortID)

			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().False(found)
				suite.Require().Empty(activeChannelID)
			} else {
				suite.Require().Error(err)
			}

		})
	}
}
