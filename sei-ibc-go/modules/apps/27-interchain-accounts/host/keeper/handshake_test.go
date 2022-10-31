package keeper_test

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"

	hosttypes "github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/host/types"
	icatypes "github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v3/modules/core/24-host"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
)

// open and close channel is a helper function for TestOnChanOpenTry for reopening accounts
func (suite *KeeperTestSuite) openAndCloseChannel(path *ibctesting.Path) {
	err := path.EndpointB.ChanOpenTry()
	suite.Require().NoError(err)

	err = path.EndpointA.ChanOpenAck()
	suite.Require().NoError(err)

	err = path.EndpointB.ChanOpenConfirm()
	suite.Require().NoError(err)

	err = path.EndpointA.SetChannelClosed()
	suite.Require().NoError(err)

	err = path.EndpointB.SetChannelClosed()
	suite.Require().NoError(err)

	path.EndpointA.ChannelID = ""
	err = RegisterInterchainAccount(path.EndpointA, TestOwnerAddress)
	suite.Require().NoError(err)

	// bump channel sequence as these test mock core IBC behaviour on ChanOpenTry
	channelSequence := path.EndpointB.Chain.App.GetIBCKeeper().ChannelKeeper.GetNextChannelSequence(path.EndpointB.Chain.GetContext())
	path.EndpointB.ChannelID = channeltypes.FormatChannelIdentifier(channelSequence)
}

func (suite *KeeperTestSuite) TestOnChanOpenTry() {
	var (
		channel  *channeltypes.Channel
		path     *ibctesting.Path
		chanCap  *capabilitytypes.Capability
		metadata icatypes.Metadata
	)

	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{
			"success",
			func() {},
			true,
		},
		{
			"success - reopening closed active channel",
			func() {
				// create interchain account
				// undo setup
				path.EndpointB.ChannelID = ""
				err := suite.chainB.App.GetScopedIBCKeeper().ReleaseCapability(suite.chainB.GetContext(), chanCap)
				suite.Require().NoError(err)

				suite.openAndCloseChannel(path)
			}, true,
		},
		{
			"success - reopening account with new address",
			func() {
				// create interchain account
				// undo setup
				path.EndpointB.ChannelID = ""
				err := suite.chainB.App.GetScopedIBCKeeper().ReleaseCapability(suite.chainB.GetContext(), chanCap)
				suite.Require().NoError(err)

				suite.openAndCloseChannel(path)

				// delete interchain account address
				store := suite.chainB.GetContext().KVStore(suite.chainB.GetSimApp().GetKey(hosttypes.SubModuleName))
				store.Delete(icatypes.KeyOwnerAccount(path.EndpointA.ChannelConfig.PortID, path.EndpointB.ConnectionID))

				// assert interchain account address mapping was deleted
				_, found := suite.chainB.GetSimApp().ICAHostKeeper.GetInterchainAccountAddress(suite.chainB.GetContext(), path.EndpointB.ConnectionID, path.EndpointA.ChannelConfig.PortID)
				suite.Require().False(found)
			}, true,
		},
		{
			"reopening account fails - no existing account",
			func() {
				// create interchain account
				// undo setup
				path.EndpointB.ChannelID = ""
				err := suite.chainB.App.GetScopedIBCKeeper().ReleaseCapability(suite.chainB.GetContext(), chanCap)
				suite.Require().NoError(err)

				suite.openAndCloseChannel(path)

				// delete existing account
				addr, found := suite.chainB.GetSimApp().ICAHostKeeper.GetInterchainAccountAddress(suite.chainB.GetContext(), path.EndpointB.ConnectionID, path.EndpointA.ChannelConfig.PortID)
				suite.Require().True(found)

				acc := suite.chainB.GetSimApp().AccountKeeper.GetAccount(suite.chainB.GetContext(), sdk.MustAccAddressFromBech32(addr))
				suite.chainB.GetSimApp().AccountKeeper.RemoveAccount(suite.chainB.GetContext(), acc)
			}, false,
		},
		{
			"reopening account fails - existing account is not interchain account type",
			func() {
				// create interchain account
				// undo setup
				path.EndpointB.ChannelID = ""
				err := suite.chainB.App.GetScopedIBCKeeper().ReleaseCapability(suite.chainB.GetContext(), chanCap)
				suite.Require().NoError(err)

				suite.openAndCloseChannel(path)

				addr, found := suite.chainB.GetSimApp().ICAHostKeeper.GetInterchainAccountAddress(suite.chainB.GetContext(), path.EndpointB.ConnectionID, path.EndpointA.ChannelConfig.PortID)
				suite.Require().True(found)

				accAddress := sdk.MustAccAddressFromBech32(addr)
				baseAcc := authtypes.NewBaseAccountWithAddress(accAddress)
				suite.chainB.GetSimApp().AccountKeeper.SetAccount(suite.chainB.GetContext(), baseAcc)
			}, false,
		},
		{
			"account already exists",
			func() {
				interchainAccAddr := icatypes.GenerateUniqueAddress(suite.chainB.GetContext(), path.EndpointB.ConnectionID, path.EndpointA.ChannelConfig.PortID)
				err := suite.chainB.GetSimApp().BankKeeper.SendCoins(suite.chainB.GetContext(), suite.chainB.SenderAccount.GetAddress(), interchainAccAddr, sdk.Coins{sdk.NewCoin("stake", sdk.NewInt(1))})
				suite.Require().NoError(err)
				suite.Require().True(suite.chainB.GetSimApp().AccountKeeper.HasAccount(suite.chainB.GetContext(), interchainAccAddr))
			},
			false,
		},
		{
			"invalid metadata - previous metadata is different",
			func() {
				// create a new channel and set it in state
				ch := channeltypes.NewChannel(channeltypes.CLOSED, channeltypes.ORDERED, channeltypes.NewCounterparty(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID), []string{path.EndpointA.ConnectionID}, TestVersion)
				suite.chainB.GetSimApp().GetIBCKeeper().ChannelKeeper.SetChannel(suite.chainB.GetContext(), path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, ch)

				// set the active channelID in state
				suite.chainB.GetSimApp().ICAHostKeeper.SetActiveChannelID(suite.chainB.GetContext(), path.EndpointB.ConnectionID, path.EndpointA.ChannelConfig.PortID, path.EndpointB.ChannelID)

				// attempt to downgrade version by reinitializing channel with version 1, but setting channel to version 2
				metadata.Version = "ics27-2"

				versionBytes, err := icatypes.ModuleCdc.MarshalJSON(&metadata)
				suite.Require().NoError(err)

				channel.Version = string(versionBytes)

				path.EndpointB.SetChannel(*channel)
			}, false,
		},
		{
			"invalid order - UNORDERED",
			func() {
				channel.Ordering = channeltypes.UNORDERED
			},
			false,
		},
		{
			"invalid port ID",
			func() {
				path.EndpointB.ChannelConfig.PortID = "invalid-port-id"
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
			"invalid metadata bytestring",
			func() {
				path.EndpointA.ChannelConfig.Version = "invalid-metadata-bytestring"
			},
			false,
		},
		{
			"unsupported encoding format",
			func() {
				metadata.Encoding = "invalid-encoding-format"

				versionBytes, err := icatypes.ModuleCdc.MarshalJSON(&metadata)
				suite.Require().NoError(err)

				path.EndpointA.ChannelConfig.Version = string(versionBytes)
			},
			false,
		},
		{
			"unsupported transaction type",
			func() {
				metadata.TxType = "invalid-tx-types"

				versionBytes, err := icatypes.ModuleCdc.MarshalJSON(&metadata)
				suite.Require().NoError(err)

				path.EndpointA.ChannelConfig.Version = string(versionBytes)
			},
			false,
		},
		{
			"invalid controller connection ID",
			func() {
				metadata.ControllerConnectionId = "invalid-connnection-id"

				versionBytes, err := icatypes.ModuleCdc.MarshalJSON(&metadata)
				suite.Require().NoError(err)

				path.EndpointA.ChannelConfig.Version = string(versionBytes)
			},
			false,
		},
		{
			"invalid host connection ID",
			func() {
				metadata.HostConnectionId = "invalid-connnection-id"

				versionBytes, err := icatypes.ModuleCdc.MarshalJSON(&metadata)
				suite.Require().NoError(err)

				path.EndpointA.ChannelConfig.Version = string(versionBytes)
			},
			false,
		},
		{
			"invalid counterparty version",
			func() {
				metadata.Version = "invalid-version"

				versionBytes, err := icatypes.ModuleCdc.MarshalJSON(&metadata)
				suite.Require().NoError(err)

				path.EndpointA.ChannelConfig.Version = string(versionBytes)
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
			"active channel already set",
			func() {
				// create a new channel and set it in state
				ch := channeltypes.NewChannel(channeltypes.OPEN, channeltypes.ORDERED, channeltypes.NewCounterparty(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID), []string{path.EndpointA.ConnectionID}, ibctesting.DefaultChannelVersion)
				suite.chainB.GetSimApp().GetIBCKeeper().ChannelKeeper.SetChannel(suite.chainB.GetContext(), path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, ch)

				// set the active channelID in state
				suite.chainB.GetSimApp().ICAHostKeeper.SetActiveChannelID(suite.chainB.GetContext(), ibctesting.FirstConnectionID, path.EndpointA.ChannelConfig.PortID, path.EndpointB.ChannelID)
			}, false,
		},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			path = NewICAPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)

			err := RegisterInterchainAccount(path.EndpointA, TestOwnerAddress)
			suite.Require().NoError(err)

			// set the channel id on host
			channelSequence := path.EndpointB.Chain.App.GetIBCKeeper().ChannelKeeper.GetNextChannelSequence(path.EndpointB.Chain.GetContext())
			path.EndpointB.ChannelID = channeltypes.FormatChannelIdentifier(channelSequence)

			// default values
			metadata = icatypes.NewMetadata(icatypes.Version, ibctesting.FirstConnectionID, ibctesting.FirstConnectionID, "", icatypes.EncodingProtobuf, icatypes.TxTypeSDKMultiMsg)
			versionBytes, err := icatypes.ModuleCdc.MarshalJSON(&metadata)
			suite.Require().NoError(err)

			counterparty := channeltypes.NewCounterparty(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			channel = &channeltypes.Channel{
				State:          channeltypes.TRYOPEN,
				Ordering:       channeltypes.ORDERED,
				Counterparty:   counterparty,
				ConnectionHops: []string{path.EndpointB.ConnectionID},
				Version:        string(versionBytes),
			}

			chanCap, err = suite.chainB.App.GetScopedIBCKeeper().NewCapability(suite.chainB.GetContext(), host.ChannelCapabilityPath(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID))
			suite.Require().NoError(err)

			tc.malleate() // malleate mutates test data

			version, err := suite.chainB.GetSimApp().ICAHostKeeper.OnChanOpenTry(suite.chainB.GetContext(), channel.Ordering, channel.GetConnectionHops(),
				path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, chanCap, channel.Counterparty, path.EndpointA.ChannelConfig.Version,
			)

			if tc.expPass {
				suite.Require().NoError(err)

				storedAddr, found := suite.chainB.GetSimApp().ICAHostKeeper.GetInterchainAccountAddress(suite.chainB.GetContext(), path.EndpointB.ConnectionID, path.EndpointA.ChannelConfig.PortID)
				suite.Require().True(found)

				interchainAccAddr, err := sdk.AccAddressFromBech32(storedAddr)
				suite.Require().NoError(err)

				// Check if account is created
				interchainAccount := suite.chainB.GetSimApp().AccountKeeper.GetAccount(suite.chainB.GetContext(), interchainAccAddr)
				suite.Require().Equal(interchainAccount.GetAddress().String(), storedAddr)
			} else {
				suite.Require().Error(err)
				suite.Require().Equal("", version)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestOnChanOpenConfirm() {
	var path *ibctesting.Path

	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{
			"success", func() {}, true,
		},
		{
			"channel not found",
			func() {
				path.EndpointB.ChannelID = "invalid-channel-id"
				path.EndpointB.ChannelConfig.PortID = "invalid-port-id"
			},
			false,
		},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			path = NewICAPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)

			err := RegisterInterchainAccount(path.EndpointA, TestOwnerAddress)
			suite.Require().NoError(err)

			err = path.EndpointB.ChanOpenTry()
			suite.Require().NoError(err)

			err = path.EndpointA.ChanOpenAck()
			suite.Require().NoError(err)

			tc.malleate() // malleate mutates test data

			err = suite.chainB.GetSimApp().ICAHostKeeper.OnChanOpenConfirm(suite.chainB.GetContext(),
				path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestOnChanCloseConfirm() {
	var path *ibctesting.Path

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

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}
