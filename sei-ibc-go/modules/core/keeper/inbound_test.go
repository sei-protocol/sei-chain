package keeper_test

import (
	"strings"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	clienttypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
	connectiontypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/03-connection/types"
	channeltypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/04-channel/types"
	host "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/24-host"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/keeper"
	ibctesting "github.com/sei-protocol/sei-chain/sei-ibc-go/testing"
)

// Test that the default configuration for inbound is enabled.
func (suite *KeeperTestSuite) TestInbound_DefaultEnabled() {
	ctx := suite.chainA.GetContext()
	ik := suite.chainA.App.GetIBCKeeper()

	enabled := ik.IsInboundEnabled(ctx)
	suite.Require().True(enabled, "expected inbound IBC to be enabled by default")
}

// Test that RecvPacket is blocked when inbound is disabled.
func (suite *KeeperTestSuite) TestRecvPacket_BlockedWhenInboundDisabled() {
	suite.SetupTest() // reset

	path := ibctesting.NewPath(suite.chainA, suite.chainB)
	path.SetChannelOrdered()
	suite.coordinator.Setup(path)

	// prepare packet from A -> B
	timeoutHeight := suite.chainB.GetTimeoutHeight()
	packet := channeltypes.NewPacket(ibctesting.MockPacketData, 1,
		path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID,
		path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID,
		timeoutHeight, 0,
	)

	err := path.EndpointA.SendPacket(packet)
	suite.Require().NoError(err)

	// disable inbound on chainB using top-level keeper
	ibcKeeperB := suite.chainB.App.GetIBCKeeper()
	ibcKeeperB.SetInboundEnabled(suite.chainB.GetContext(), false)
	suite.Require().False(ibcKeeperB.IsInboundEnabled(suite.chainB.GetContext()), "expected inbound to be disabled")

	// fetch proof of packet commitment from chainA
	packetKey := host.PacketCommitmentKey(packet.GetSourcePort(), packet.GetSourceChannel(), packet.GetSequence())
	proof, proofHeight := path.EndpointA.QueryProof(packetKey)

	// craft MsgRecvPacket as other tests do
	msg := channeltypes.NewMsgRecvPacket(packet, proof, proofHeight, suite.chainB.SenderAccount.GetAddress().String())

	// call the gRPC/keeper RecvPacket and expect an error because inbound is disabled
	_, err = keeper.Keeper.RecvPacket(*ibcKeeperB, sdk.WrapSDKContext(suite.chainB.GetContext()), msg)
	suite.Require().Error(err, "expected RecvPacket to return an error when inbound is disabled")
	suite.Require().True(strings.Contains(strings.ToLower(err.Error()), "inbound"),
		"expected error to mention inbound/disabled, got: %s", err.Error())
}

// TestAcknowledgementAllowedWhenOutboundDisabled verifies that MsgAcknowledgement
// succeeds even when OutboundEnabled == false (settlement must be allowed).
func (suite *KeeperTestSuite) TestAcknowledgementAllowedWhenOutboundDisabled() {
	suite.SetupTest()

	path := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.Setup(path)

	// send packet from A -> B
	timeoutHeight := suite.chainB.GetTimeoutHeight()
	packet := channeltypes.NewPacket(ibctesting.MockPacketData, 1,
		path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID,
		path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID,
		timeoutHeight, 0,
	)
	err := path.EndpointA.SendPacket(packet)
	suite.Require().NoError(err)

	// receive packet on B (creates ack)
	err = path.EndpointB.RecvPacket(packet)
	suite.Require().NoError(err)

	// disable outbound on chainA
	ibcKeeperA := suite.chainA.App.GetIBCKeeper()
	ibcKeeperA.SetOutboundEnabled(suite.chainA.GetContext(), false)
	suite.Require().False(ibcKeeperA.IsOutboundEnabled(suite.chainA.GetContext()))

	// ack should still succeed
	err = path.EndpointA.AcknowledgePacket(packet, ibctesting.MockAcknowledgement)
	suite.Require().NoError(err, "MsgAcknowledgement should succeed when outbound is disabled")
}

// TestTimeoutAllowedWhenOutboundDisabled verifies that MsgTimeout
// succeeds even when OutboundEnabled == false (settlement must be allowed).
func (suite *KeeperTestSuite) TestTimeoutAllowedWhenOutboundDisabled() {
	suite.SetupTest()

	path := ibctesting.NewPath(suite.chainA, suite.chainB)
	path.SetChannelOrdered()
	suite.coordinator.Setup(path)

	// send packet from A -> B with a low timeout height
	packet := channeltypes.NewPacket(ibctesting.MockPacketData, 1,
		path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID,
		path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID,
		clienttypes.GetSelfHeight(suite.chainB.GetContext()), 0,
	)
	err := path.EndpointA.SendPacket(packet)
	suite.Require().NoError(err)

	// advance chainB past the timeout height
	suite.coordinator.CommitNBlocks(suite.chainB, 3)
	path.EndpointA.UpdateClient()

	// disable outbound on chainA
	ibcKeeperA := suite.chainA.App.GetIBCKeeper()
	ibcKeeperA.SetOutboundEnabled(suite.chainA.GetContext(), false)
	suite.Require().False(ibcKeeperA.IsOutboundEnabled(suite.chainA.GetContext()))

	// timeout should still succeed
	err = path.EndpointA.TimeoutPacket(packet)
	suite.Require().NoError(err, "MsgTimeout should succeed when outbound is disabled")
}

// TestConnectionOpenInit_BlockedWhenOutboundDisabled tests that MsgConnectionOpenInit
// is blocked when outbound IBC is disabled.
func (suite *KeeperTestSuite) TestConnectionOpenInit_BlockedWhenOutboundDisabled() {
	suite.SetupTest()

	path := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.SetupClients(path)

	// disable outbound on chainA
	ibcKeeperA := suite.chainA.App.GetIBCKeeper()
	ibcKeeperA.SetOutboundEnabled(suite.chainA.GetContext(), false)
	suite.Require().False(ibcKeeperA.IsOutboundEnabled(suite.chainA.GetContext()))

	// craft MsgConnectionOpenInit
	msg := connectiontypes.NewMsgConnectionOpenInit(
		path.EndpointA.ClientID, path.EndpointB.ClientID, suite.chainB.GetPrefix(),
		connectiontypes.DefaultIBCVersion, 0,
		suite.chainA.SenderAccount.GetAddress().String(),
	)

	// call the gRPC/keeper ConnectionOpenInit and expect an error
	_, err := keeper.Keeper.ConnectionOpenInit(*ibcKeeperA, sdk.WrapSDKContext(suite.chainA.GetContext()), msg)
	suite.Require().Error(err, "expected ConnectionOpenInit to return an error when outbound is disabled")
	suite.Require().True(strings.Contains(strings.ToLower(err.Error()), "outbound"),
		"expected error to mention outbound, got: %s", err.Error())
}

// TestChannelOpenInit_BlockedWhenOutboundDisabled tests that MsgChannelOpenInit
// is blocked when outbound IBC is disabled.
func (suite *KeeperTestSuite) TestChannelOpenInit_BlockedWhenOutboundDisabled() {
	suite.SetupTest()

	path := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.SetupConnections(path)
	path.SetChannelOrdered()

	// disable outbound on chainA
	ibcKeeperA := suite.chainA.App.GetIBCKeeper()
	ibcKeeperA.SetOutboundEnabled(suite.chainA.GetContext(), false)
	suite.Require().False(ibcKeeperA.IsOutboundEnabled(suite.chainA.GetContext()))

	// craft MsgChannelOpenInit
	msg := channeltypes.NewMsgChannelOpenInit(
		path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelConfig.Version,
		channeltypes.ORDERED, []string{path.EndpointA.ConnectionID},
		ibctesting.MockPort, suite.chainA.SenderAccount.GetAddress().String(),
	)

	// call the gRPC/keeper ChannelOpenInit and expect an error
	_, err := keeper.Keeper.ChannelOpenInit(*ibcKeeperA, sdk.WrapSDKContext(suite.chainA.GetContext()), msg)
	suite.Require().Error(err, "expected ChannelOpenInit to return an error when outbound is disabled")
	suite.Require().True(strings.Contains(strings.ToLower(err.Error()), "outbound"),
		"expected error to mention outbound, got: %s", err.Error())
}
