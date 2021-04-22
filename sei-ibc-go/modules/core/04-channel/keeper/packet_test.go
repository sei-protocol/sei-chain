package keeper_test

import (
	"fmt"

	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"
	clienttypes "github.com/cosmos/ibc-go/modules/core/02-client/types"
	"github.com/cosmos/ibc-go/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/modules/core/24-host"
	"github.com/cosmos/ibc-go/modules/core/exported"
	ibctmtypes "github.com/cosmos/ibc-go/modules/light-clients/07-tendermint/types"
	ibctesting "github.com/cosmos/ibc-go/testing"
	ibcmock "github.com/cosmos/ibc-go/testing/mock"
)

var (
	disabledTimeoutTimestamp = uint64(0)
	disabledTimeoutHeight    = clienttypes.ZeroHeight()
	timeoutHeight            = clienttypes.NewHeight(0, 100)

	// for when the testing package cannot be used
	clientIDA  = "clientA"
	clientIDB  = "clientB"
	connIDA    = "connA"
	connIDB    = "connB"
	portID     = "portid"
	channelIDA = "channelidA"
	channelIDB = "channelidB"
)

// TestSendPacket tests SendPacket from chainA to chainB
func (suite *KeeperTestSuite) TestSendPacket() {
	var (
		path       *ibctesting.Path
		packet     exported.PacketI
		channelCap *capabilitytypes.Capability
	)

	testCases := []testCase{
		{"success: UNORDERED channel", func() {
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, true},
		{"success: ORDERED channel", func() {
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, true},
		{"sending packet out of order on UNORDERED channel", func() {
			// setup creates an unordered channel
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 5, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"sending packet out of order on ORDERED channel", func() {
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 5, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"packet basic validation failed, empty packet data", func() {
			suite.coordinator.Setup(path)
			packet = types.NewPacket([]byte{}, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"channel not found", func() {
			// use wrong channel naming
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, ibctesting.InvalidID, ibctesting.InvalidID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"channel closed", func() {
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)

			err := path.EndpointA.SetChannelClosed()
			suite.Require().NoError(err)
		}, false},
		{"packet dest port ≠ channel counterparty port", func() {
			suite.coordinator.Setup(path)
			// use wrong port for dest
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, ibctesting.InvalidID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"packet dest channel ID ≠ channel counterparty channel ID", func() {
			suite.coordinator.Setup(path)
			// use wrong channel for dest
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, ibctesting.InvalidID, timeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"connection not found", func() {
			// pass channel check
			suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetChannel(
				suite.chainA.GetContext(),
				path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID,
				types.NewChannel(types.OPEN, types.ORDERED, types.NewCounterparty(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID), []string{connIDA}, path.EndpointA.ChannelConfig.Version),
			)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			suite.chainA.CreateChannelCapability(suite.chainA.GetSimApp().ScopedIBCMockKeeper, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"client state not found", func() {
			suite.coordinator.Setup(path)

			// change connection client ID
			connection := path.EndpointA.GetConnection()
			connection.ClientId = ibctesting.InvalidID
			suite.chainA.App.GetIBCKeeper().ConnectionKeeper.SetConnection(suite.chainA.GetContext(), path.EndpointA.ConnectionID, connection)

			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"client state is frozen", func() {
			suite.coordinator.Setup(path)

			connection := path.EndpointA.GetConnection()
			clientState := path.EndpointA.GetClientState()
			cs, ok := clientState.(*ibctmtypes.ClientState)
			suite.Require().True(ok)

			// freeze client
			cs.FrozenHeight = clienttypes.NewHeight(0, 1)
			suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientState(suite.chainA.GetContext(), connection.ClientId, cs)

			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},

		{"timeout height passed", func() {
			suite.coordinator.Setup(path)
			// use client state latest height for timeout
			clientState := path.EndpointA.GetClientState()
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clientState.GetLatestHeight().(clienttypes.Height), disabledTimeoutTimestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"timeout timestamp passed", func() {
			suite.coordinator.Setup(path)
			// use latest time on client state
			clientState := path.EndpointA.GetClientState()
			connection := path.EndpointA.GetConnection()
			timestamp, err := suite.chainA.App.GetIBCKeeper().ConnectionKeeper.GetTimestampAtHeight(suite.chainA.GetContext(), connection, clientState.GetLatestHeight())
			suite.Require().NoError(err)

			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, disabledTimeoutHeight, timestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"next sequence send not found", func() {
			path := ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			// manually creating channel prevents next sequence from being set
			suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetChannel(
				suite.chainA.GetContext(),
				path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID,
				types.NewChannel(types.OPEN, types.ORDERED, types.NewCounterparty(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID), []string{path.EndpointA.ConnectionID}, path.EndpointA.ChannelConfig.Version),
			)
			suite.chainA.CreateChannelCapability(suite.chainA.GetSimApp().ScopedIBCMockKeeper, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"next sequence wrong", func() {
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetNextSequenceSend(suite.chainA.GetContext(), path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, 5)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"channel capability not found", func() {
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			channelCap = capabilitytypes.NewCapability(5)
		}, false},
	}

	for i, tc := range testCases {
		tc := tc
		suite.Run(fmt.Sprintf("Case %s, %d/%d tests", tc.msg, i, len(testCases)), func() {
			suite.SetupTest() // reset
			path = ibctesting.NewPath(suite.chainA, suite.chainB)

			tc.malleate()

			err := suite.chainA.App.GetIBCKeeper().ChannelKeeper.SendPacket(suite.chainA.GetContext(), channelCap, packet)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}

}

// TestRecvPacket test RecvPacket on chainB. Since packet commitment verification will always
// occur last (resource instensive), only tests expected to succeed and packet commitment
// verification tests need to simulate sending a packet from chainA to chainB.
func (suite *KeeperTestSuite) TestRecvPacket() {
	var (
		path       *ibctesting.Path
		packet     exported.PacketI
		channelCap *capabilitytypes.Capability
	)

	testCases := []testCase{
		{"success: ORDERED channel", func() {
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)

			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			err := path.EndpointA.SendPacket(packet)
			suite.Require().NoError(err)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, true},
		{"success UNORDERED channel", func() {
			// setup uses an UNORDERED channel
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			err := path.EndpointA.SendPacket(packet)
			suite.Require().NoError(err)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, true},
		{"success with out of order packet: UNORDERED channel", func() {
			// setup uses an UNORDERED channel
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)

			// send 2 packets
			err := path.EndpointA.SendPacket(packet)
			suite.Require().NoError(err)
			// set sequence to 2
			packet = types.NewPacket(ibctesting.MockPacketData, 2, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			err = path.EndpointA.SendPacket(packet)
			suite.Require().NoError(err)
			// attempts to receive packet 2 without receiving packet 1
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, true},
		{"out of order packet failure with ORDERED channel", func() {
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)

			// send 2 packets
			err := path.EndpointA.SendPacket(packet)
			suite.Require().NoError(err)
			// set sequence to 2
			packet = types.NewPacket(ibctesting.MockPacketData, 2, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			err = path.EndpointA.SendPacket(packet)
			suite.Require().NoError(err)
			// attempts to receive packet 2 without receiving packet 1
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"channel not found", func() {
			// use wrong channel naming
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, ibctesting.InvalidID, ibctesting.InvalidID, timeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"channel not open", func() {
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)

			err := path.EndpointB.SetChannelClosed()
			suite.Require().NoError(err)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"capability cannot authenticate ORDERED", func() {
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			err := path.EndpointA.SendPacket(packet)
			suite.Require().NoError(err)
			channelCap = capabilitytypes.NewCapability(3)
		}, false},
		{"packet source port ≠ channel counterparty port", func() {
			suite.coordinator.Setup(path)
			// use wrong port for dest
			packet = types.NewPacket(ibctesting.MockPacketData, 1, ibctesting.InvalidID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"packet source channel ID ≠ channel counterparty channel ID", func() {
			suite.coordinator.Setup(path)
			// use wrong port for dest
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, ibctesting.InvalidID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"connection not found", func() {
			// pass channel check
			suite.chainB.App.GetIBCKeeper().ChannelKeeper.SetChannel(
				suite.chainB.GetContext(),
				path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID,
				types.NewChannel(types.OPEN, types.ORDERED, types.NewCounterparty(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID), []string{connIDB}, path.EndpointB.ChannelConfig.Version),
			)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			suite.chainB.CreateChannelCapability(suite.chainB.GetSimApp().ScopedIBCMockKeeper, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"connection not OPEN", func() {
			suite.coordinator.SetupClients(path)

			// connection on chainB is in INIT
			err := path.EndpointB.ConnOpenInit()
			suite.Require().NoError(err)

			// pass channel check
			suite.chainB.App.GetIBCKeeper().ChannelKeeper.SetChannel(
				suite.chainB.GetContext(),
				path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID,
				types.NewChannel(types.OPEN, types.ORDERED, types.NewCounterparty(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID), []string{path.EndpointB.ConnectionID}, path.EndpointB.ChannelConfig.Version),
			)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			suite.chainB.CreateChannelCapability(suite.chainB.GetSimApp().ScopedIBCMockKeeper, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"timeout height passed", func() {
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.GetSelfHeight(suite.chainB.GetContext()), disabledTimeoutTimestamp)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"timeout timestamp passed", func() {
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, disabledTimeoutHeight, uint64(suite.chainB.GetContext().BlockTime().UnixNano()))
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"next receive sequence is not found", func() {
			path := ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)

			// manually creating channel prevents next recv sequence from being set
			suite.chainB.App.GetIBCKeeper().ChannelKeeper.SetChannel(
				suite.chainB.GetContext(),
				path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID,
				types.NewChannel(types.OPEN, types.ORDERED, types.NewCounterparty(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID), []string{path.EndpointB.ConnectionID}, path.EndpointB.ChannelConfig.Version),
			)

			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)

			// manually set packet commitment
			suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetPacketCommitment(suite.chainA.GetContext(), path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, packet.GetSequence(), ibctesting.MockPacketData)
			suite.chainB.CreateChannelCapability(suite.chainB.GetSimApp().ScopedIBCMockKeeper, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)

			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"receipt already stored", func() {
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			path.EndpointA.SendPacket(packet)
			suite.chainB.App.GetIBCKeeper().ChannelKeeper.SetPacketReceipt(suite.chainB.GetContext(), path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, 1)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"validation failed", func() {
			// packet commitment not set resulting in invalid proof
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
	}

	for i, tc := range testCases {
		tc := tc
		suite.Run(fmt.Sprintf("Case %s, %d/%d tests", tc.msg, i, len(testCases)), func() {
			suite.SetupTest() // reset
			path = ibctesting.NewPath(suite.chainA, suite.chainB)

			tc.malleate()

			// get proof of packet commitment from chainA
			packetKey := host.PacketCommitmentKey(packet.GetSourcePort(), packet.GetSourceChannel(), packet.GetSequence())
			proof, proofHeight := suite.chainA.QueryProof(packetKey)

			err := suite.chainB.App.GetIBCKeeper().ChannelKeeper.RecvPacket(suite.chainB.GetContext(), channelCap, packet, proof, proofHeight)

			if tc.expPass {
				suite.Require().NoError(err)

				channelB, _ := suite.chainB.App.GetIBCKeeper().ChannelKeeper.GetChannel(suite.chainB.GetContext(), packet.GetDestPort(), packet.GetDestChannel())
				nextSeqRecv, found := suite.chainB.App.GetIBCKeeper().ChannelKeeper.GetNextSequenceRecv(suite.chainB.GetContext(), packet.GetDestPort(), packet.GetDestChannel())
				suite.Require().True(found)
				receipt, receiptStored := suite.chainB.App.GetIBCKeeper().ChannelKeeper.GetPacketReceipt(suite.chainB.GetContext(), packet.GetDestPort(), packet.GetDestChannel(), packet.GetSequence())

				if channelB.Ordering == types.ORDERED {
					suite.Require().Equal(packet.GetSequence()+1, nextSeqRecv, "sequence not incremented in ordered channel")
					suite.Require().False(receiptStored, "packet receipt stored on ORDERED channel")
				} else {
					suite.Require().Equal(uint64(1), nextSeqRecv, "sequence incremented for UNORDERED channel")
					suite.Require().True(receiptStored, "packet receipt not stored after RecvPacket in UNORDERED channel")
					suite.Require().Equal(string([]byte{byte(1)}), receipt, "packet receipt is not empty string")
				}
			} else {
				suite.Require().Error(err)
			}
		})
	}

}

func (suite *KeeperTestSuite) TestWriteAcknowledgement() {
	var (
		path       *ibctesting.Path
		ack        []byte
		packet     exported.PacketI
		channelCap *capabilitytypes.Capability
	)

	testCases := []testCase{
		{
			"success",
			func() {
				suite.coordinator.Setup(path)
				packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
				ack = ibctesting.MockAcknowledgement
				channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
			},
			true,
		},
		{"channel not found", func() {
			// use wrong channel naming
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, ibctesting.InvalidID, ibctesting.InvalidID, timeoutHeight, disabledTimeoutTimestamp)
			ack = ibctesting.MockAcknowledgement
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"channel not open", func() {
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			ack = ibctesting.MockAcknowledgement

			err := path.EndpointB.SetChannelClosed()
			suite.Require().NoError(err)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{
			"capability authentication failed",
			func() {
				suite.coordinator.Setup(path)
				packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
				ack = ibctesting.MockAcknowledgement
				channelCap = capabilitytypes.NewCapability(3)
			},
			false,
		},
		{
			"no-op, already acked",
			func() {
				suite.coordinator.Setup(path)
				packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
				ack = ibctesting.MockAcknowledgement
				suite.chainB.App.GetIBCKeeper().ChannelKeeper.SetPacketAcknowledgement(suite.chainB.GetContext(), packet.GetDestPort(), packet.GetDestChannel(), packet.GetSequence(), ack)
				channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
			},
			false,
		},
		{
			"empty acknowledgement",
			func() {
				suite.coordinator.Setup(path)
				packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
				ack = nil
				channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
			},
			false,
		},
	}
	for i, tc := range testCases {
		tc := tc
		suite.Run(fmt.Sprintf("Case %s, %d/%d tests", tc.msg, i, len(testCases)), func() {
			suite.SetupTest() // reset
			path = ibctesting.NewPath(suite.chainA, suite.chainB)

			tc.malleate()

			err := suite.chainB.App.GetIBCKeeper().ChannelKeeper.WriteAcknowledgement(suite.chainB.GetContext(), channelCap, packet, ack)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

// TestAcknowledgePacket tests the call AcknowledgePacket on chainA.
func (suite *KeeperTestSuite) TestAcknowledgePacket() {
	var (
		path   *ibctesting.Path
		packet types.Packet
		ack    = ibcmock.MockAcknowledgement

		channelCap *capabilitytypes.Capability
	)

	testCases := []testCase{
		{"success on ordered channel", func() {
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			// create packet commitment
			err := path.EndpointA.SendPacket(packet)
			suite.Require().NoError(err)

			// create packet receipt and acknowledgement
			err = path.EndpointB.RecvPacket(packet)
			suite.Require().NoError(err)

			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, true},
		{"success on unordered channel", func() {
			// setup uses an UNORDERED channel
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)

			// create packet commitment
			err := path.EndpointA.SendPacket(packet)
			suite.Require().NoError(err)

			// create packet receipt and acknowledgement
			err = path.EndpointB.RecvPacket(packet)
			suite.Require().NoError(err)

			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, true},
		{"channel not found", func() {
			// use wrong channel naming
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, ibctesting.InvalidID, ibctesting.InvalidID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
		}, false},
		{"channel not open", func() {
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)

			err := path.EndpointA.SetChannelClosed()
			suite.Require().NoError(err)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"capability authentication failed ORDERED", func() {
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			// create packet commitment
			err := path.EndpointA.SendPacket(packet)
			suite.Require().NoError(err)

			// create packet receipt and acknowledgement
			err = path.EndpointB.RecvPacket(packet)
			suite.Require().NoError(err)

			channelCap = capabilitytypes.NewCapability(3)
		}, false},
		{"packet destination port ≠ channel counterparty port", func() {
			suite.coordinator.Setup(path)
			// use wrong port for dest
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, ibctesting.InvalidID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"packet destination channel ID ≠ channel counterparty channel ID", func() {
			suite.coordinator.Setup(path)
			// use wrong channel for dest
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, ibctesting.InvalidID, timeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"connection not found", func() {
			// pass channel check
			suite.chainB.App.GetIBCKeeper().ChannelKeeper.SetChannel(
				suite.chainB.GetContext(),
				path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID,
				types.NewChannel(types.OPEN, types.ORDERED, types.NewCounterparty(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID), []string{connIDB}, path.EndpointB.ChannelConfig.Version),
			)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			suite.chainA.CreateChannelCapability(suite.chainA.GetSimApp().ScopedIBCMockKeeper, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"connection not OPEN", func() {
			suite.coordinator.SetupClients(path)
			// connection on chainA is in INIT
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// pass channel check
			suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetChannel(
				suite.chainA.GetContext(),
				path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID,
				types.NewChannel(types.OPEN, types.ORDERED, types.NewCounterparty(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID), []string{path.EndpointA.ConnectionID}, path.EndpointA.ChannelConfig.Version),
			)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			suite.chainA.CreateChannelCapability(suite.chainA.GetSimApp().ScopedIBCMockKeeper, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"packet hasn't been sent", func() {
			// packet commitment never written
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"packet ack verification failed", func() {
			// ack never written
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)

			// create packet commitment
			path.EndpointA.SendPacket(packet)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"next ack sequence not found", func() {
			path := ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			// manually creating channel prevents next sequence acknowledgement from being set
			suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetChannel(
				suite.chainA.GetContext(),
				path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID,
				types.NewChannel(types.OPEN, types.ORDERED, types.NewCounterparty(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID), []string{path.EndpointA.ConnectionID}, path.EndpointA.ChannelConfig.Version),
			)
			// manually set packet commitment
			suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetPacketCommitment(suite.chainA.GetContext(), path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, packet.GetSequence(), ibctesting.MockPacketData)

			// manually set packet acknowledgement and capability
			suite.chainB.App.GetIBCKeeper().ChannelKeeper.SetPacketAcknowledgement(suite.chainB.GetContext(), path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, packet.GetSequence(), ibctesting.MockAcknowledgement)
			suite.chainA.CreateChannelCapability(suite.chainA.GetSimApp().ScopedIBCMockKeeper, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"next ack sequence mismatch ORDERED", func() {
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			// create packet commitment
			err := path.EndpointA.SendPacket(packet)
			suite.Require().NoError(err)

			// create packet acknowledgement
			err = path.EndpointB.RecvPacket(packet)
			suite.Require().NoError(err)

			// set next sequence ack wrong
			suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetNextSequenceAck(suite.chainA.GetContext(), path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, 10)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
	}

	for i, tc := range testCases {
		tc := tc
		suite.Run(fmt.Sprintf("Case %s, %d/%d tests", tc.msg, i, len(testCases)), func() {
			suite.SetupTest() // reset
			path = ibctesting.NewPath(suite.chainA, suite.chainB)

			tc.malleate()

			packetKey := host.PacketAcknowledgementKey(packet.GetDestPort(), packet.GetDestChannel(), packet.GetSequence())
			proof, proofHeight := suite.chainB.QueryProof(packetKey)

			err := suite.chainA.App.GetIBCKeeper().ChannelKeeper.AcknowledgePacket(suite.chainA.GetContext(), channelCap, packet, ack.Acknowledgement(), proof, proofHeight)
			pc := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetPacketCommitment(suite.chainA.GetContext(), packet.GetSourcePort(), packet.GetSourceChannel(), packet.GetSequence())

			channelA, _ := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetChannel(suite.chainA.GetContext(), packet.GetSourcePort(), packet.GetSourceChannel())
			sequenceAck, _ := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetNextSequenceAck(suite.chainA.GetContext(), packet.GetSourcePort(), packet.GetSourceChannel())

			if tc.expPass {
				suite.NoError(err)
				suite.Nil(pc)

				if channelA.Ordering == types.ORDERED {
					suite.Require().Equal(packet.GetSequence()+1, sequenceAck, "sequence not incremented in ordered channel")
				} else {
					suite.Require().Equal(uint64(1), sequenceAck, "sequence incremented for UNORDERED channel")
				}
			} else {
				suite.Error(err)
			}
		})
	}
}
