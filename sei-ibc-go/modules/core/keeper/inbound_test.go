package keeper_test

import (
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

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

