package keeper_test

import (
	"fmt"
	"time"

	clienttypes "github.com/cosmos/ibc-go/modules/core/02-client/types"
	"github.com/cosmos/ibc-go/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/modules/core/24-host"
	"github.com/cosmos/ibc-go/modules/core/exported"
	ibctmtypes "github.com/cosmos/ibc-go/modules/light-clients/07-tendermint/types"
	ibctesting "github.com/cosmos/ibc-go/testing"
	ibcmock "github.com/cosmos/ibc-go/testing/mock"
)

var defaultTimeoutHeight = clienttypes.NewHeight(0, 100000)

// TestVerifyClientState verifies a client state of chainA
// stored on path.EndpointB (which is on chainB)
func (suite *KeeperTestSuite) TestVerifyClientState() {
	cases := []struct {
		msg                  string
		changeClientID       bool
		heightDiff           uint64
		malleateCounterparty bool
		expPass              bool
	}{
		{"verification success", false, 0, false, true},
		{"client state not found", true, 0, false, false},
		{"consensus state for proof height not found", false, 5, false, false},
		{"verification failed", false, 0, true, false},
	}

	for _, tc := range cases {
		tc := tc

		suite.Run(tc.msg, func() {
			suite.SetupTest() // reset

			path := ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)

			counterpartyClient, clientProof := path.EndpointB.QueryClientStateProof()
			proofHeight := clienttypes.NewHeight(0, uint64(suite.chainB.GetContext().BlockHeight()-1))

			if tc.malleateCounterparty {
				tmClient, _ := counterpartyClient.(*ibctmtypes.ClientState)
				tmClient.ChainId = "wrongChainID"
			}

			connection := path.EndpointA.GetConnection()
			if tc.changeClientID {
				connection.ClientId = ibctesting.InvalidID
			}

			err := suite.chainA.App.GetIBCKeeper().ConnectionKeeper.VerifyClientState(
				suite.chainA.GetContext(), connection,
				malleateHeight(proofHeight, tc.heightDiff), clientProof, counterpartyClient,
			)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

// TestVerifyClientConsensusState verifies that the consensus state of
// chainA stored on path.EndpointB.ClientID (which is on chainB) matches the consensus
// state for chainA at that height.
func (suite *KeeperTestSuite) TestVerifyClientConsensusState() {
	var (
		path           *ibctesting.Path
		changeClientID bool
		heightDiff     uint64
	)
	cases := []struct {
		msg      string
		malleate func()
		expPass  bool
	}{
		{"verification success", func() {
		}, true},
		{"client state not found", func() {
			changeClientID = true
		}, false},
		{"consensus state not found", func() {
			heightDiff = 5
		}, false},
		{"verification failed", func() {
			clientState := suite.chainB.GetClientState(path.EndpointB.ClientID)

			// give chainB wrong consensus state for chainA
			consState, found := suite.chainB.App.GetIBCKeeper().ClientKeeper.GetLatestClientConsensusState(suite.chainB.GetContext(), path.EndpointB.ClientID)
			suite.Require().True(found)

			tmConsState, ok := consState.(*ibctmtypes.ConsensusState)
			suite.Require().True(ok)

			tmConsState.Timestamp = time.Now()
			suite.chainB.App.GetIBCKeeper().ClientKeeper.SetClientConsensusState(suite.chainB.GetContext(), path.EndpointB.ClientID, clientState.GetLatestHeight(), tmConsState)

			suite.coordinator.CommitBlock(suite.chainB)
		}, false},
	}

	for _, tc := range cases {
		tc := tc

		suite.Run(tc.msg, func() {
			suite.SetupTest()      // reset
			heightDiff = 0         // must be explicitly changed in malleate
			changeClientID = false // must be explicitly changed in malleate
			path = ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)

			tc.malleate()

			connection := path.EndpointA.GetConnection()
			if changeClientID {
				connection.ClientId = ibctesting.InvalidID
			}

			proof, consensusHeight := suite.chainB.QueryConsensusStateProof(path.EndpointB.ClientID)
			proofHeight := clienttypes.NewHeight(0, uint64(suite.chainB.GetContext().BlockHeight()-1))
			consensusState, found := suite.chainA.App.GetIBCKeeper().ClientKeeper.GetSelfConsensusState(suite.chainA.GetContext(), consensusHeight)
			suite.Require().True(found)

			err := suite.chainA.App.GetIBCKeeper().ConnectionKeeper.VerifyClientConsensusState(
				suite.chainA.GetContext(), connection,
				malleateHeight(proofHeight, heightDiff), consensusHeight, proof, consensusState,
			)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

// TestVerifyConnectionState verifies the connection state of the connection
// on chainB. The connections on chainA and chainB are fully opened.
func (suite *KeeperTestSuite) TestVerifyConnectionState() {
	cases := []struct {
		msg                   string
		changeClientID        bool
		changeConnectionState bool
		heightDiff            uint64
		expPass               bool
	}{
		{"verification success", false, false, 0, true},
		{"client state not found - changed client ID", true, false, 0, false},
		{"consensus state not found - increased proof height", false, false, 5, false},
		{"verification failed - connection state is different than proof", false, true, 0, false},
	}

	for _, tc := range cases {
		tc := tc

		suite.Run(tc.msg, func() {
			suite.SetupTest() // reset

			path := ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)

			connection := path.EndpointA.GetConnection()
			if tc.changeClientID {
				connection.ClientId = ibctesting.InvalidID
			}
			expectedConnection := path.EndpointB.GetConnection()

			connectionKey := host.ConnectionKey(path.EndpointB.ConnectionID)
			proof, proofHeight := suite.chainB.QueryProof(connectionKey)

			if tc.changeConnectionState {
				expectedConnection.State = types.TRYOPEN
			}

			err := suite.chainA.App.GetIBCKeeper().ConnectionKeeper.VerifyConnectionState(
				suite.chainA.GetContext(), connection,
				malleateHeight(proofHeight, tc.heightDiff), proof, path.EndpointB.ConnectionID, expectedConnection,
			)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

// TestVerifyChannelState verifies the channel state of the channel on
// chainB. The channels on chainA and chainB are fully opened.
func (suite *KeeperTestSuite) TestVerifyChannelState() {
	cases := []struct {
		msg                string
		changeClientID     bool
		changeChannelState bool
		heightDiff         uint64
		expPass            bool
	}{
		{"verification success", false, false, 0, true},
		{"client state not found- changed client ID", true, false, 0, false},
		{"consensus state not found - increased proof height", false, false, 5, false},
		{"verification failed - changed channel state", false, true, 0, false},
	}

	for _, tc := range cases {
		tc := tc

		suite.Run(fmt.Sprintf("Case %s", tc.msg), func() {
			suite.SetupTest() // reset

			path := ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.Setup(path)
			connection := path.EndpointA.GetConnection()
			if tc.changeClientID {
				connection.ClientId = ibctesting.InvalidID
			}

			channelKey := host.ChannelKey(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
			proof, proofHeight := suite.chainB.QueryProof(channelKey)

			channel := path.EndpointB.GetChannel()
			if tc.changeChannelState {
				channel.State = channeltypes.TRYOPEN
			}

			err := suite.chainA.App.GetIBCKeeper().ConnectionKeeper.VerifyChannelState(
				suite.chainA.GetContext(), connection, malleateHeight(proofHeight, tc.heightDiff), proof,
				path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, channel,
			)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

// TestVerifyPacketCommitmentState has chainB verify the packet commitment
// on channelA. The channels on chainA and chainB are fully opened and a
// packet is sent from chainA to chainB, but has not been received.
func (suite *KeeperTestSuite) TestVerifyPacketCommitment() {
	cases := []struct {
		msg                         string
		changeClientID              bool
		changePacketCommitmentState bool
		heightDiff                  uint64
		delayPeriod                 uint64
		expPass                     bool
	}{
		{"verification success", false, false, 0, 0, true},
		{"verification success: delay period passed", false, false, 0, uint64(1 * time.Second.Nanoseconds()), true},
		{"delay period has not passed", false, false, 0, uint64(1 * time.Hour.Nanoseconds()), false},
		{"client state not found- changed client ID", true, false, 0, 0, false},
		{"consensus state not found - increased proof height", false, false, 5, 0, false},
		{"verification failed - changed packet commitment state", false, true, 0, 0, false},
	}

	for _, tc := range cases {
		tc := tc

		suite.Run(tc.msg, func() {
			suite.SetupTest() // reset

			path := ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.Setup(path)

			connection := path.EndpointB.GetConnection()
			connection.DelayPeriod = tc.delayPeriod
			if tc.changeClientID {
				connection.ClientId = ibctesting.InvalidID
			}

			packet := channeltypes.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, 0)
			err := path.EndpointA.SendPacket(packet)
			suite.Require().NoError(err)

			commitmentKey := host.PacketCommitmentKey(packet.GetSourcePort(), packet.GetSourceChannel(), packet.GetSequence())
			proof, proofHeight := suite.chainA.QueryProof(commitmentKey)

			if tc.changePacketCommitmentState {
				packet.Data = []byte(ibctesting.InvalidID)
			}

			commitment := channeltypes.CommitPacket(suite.chainB.App.GetIBCKeeper().Codec(), packet)
			err = suite.chainB.App.GetIBCKeeper().ConnectionKeeper.VerifyPacketCommitment(
				suite.chainB.GetContext(), connection, malleateHeight(proofHeight, tc.heightDiff), proof,
				packet.GetSourcePort(), packet.GetSourceChannel(), packet.GetSequence(), commitment,
			)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

// TestVerifyPacketAcknowledgement has chainA verify the acknowledgement on
// channelB. The channels on chainA and chainB are fully opened and a packet
// is sent from chainA to chainB and received.
func (suite *KeeperTestSuite) TestVerifyPacketAcknowledgement() {
	cases := []struct {
		msg                   string
		changeClientID        bool
		changeAcknowledgement bool
		heightDiff            uint64
		delayPeriod           uint64
		expPass               bool
	}{
		{"verification success", false, false, 0, 0, true},
		{"verification success: delay period passed", false, false, 0, uint64(1 * time.Second.Nanoseconds()), true},
		{"delay period has not passed", false, false, 0, uint64(1 * time.Hour.Nanoseconds()), false},
		{"client state not found- changed client ID", true, false, 0, 0, false},
		{"consensus state not found - increased proof height", false, false, 5, 0, false},
		{"verification failed - changed acknowledgement", false, true, 0, 0, false},
	}

	for _, tc := range cases {
		tc := tc

		suite.Run(tc.msg, func() {
			suite.SetupTest() // reset

			path := ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.Setup(path)

			connection := path.EndpointA.GetConnection()
			connection.DelayPeriod = tc.delayPeriod
			if tc.changeClientID {
				connection.ClientId = ibctesting.InvalidID
			}

			// send and receive packet
			packet := channeltypes.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, 0)
			err := path.EndpointA.SendPacket(packet)
			suite.Require().NoError(err)

			// increment receiving chain's (chainB) time by 2 hour to always pass receive
			suite.coordinator.IncrementTimeBy(time.Hour * 2)
			suite.coordinator.CommitBlock(suite.chainB)

			err = path.EndpointB.RecvPacket(packet)
			suite.Require().NoError(err)

			packetAckKey := host.PacketAcknowledgementKey(packet.GetDestPort(), packet.GetDestChannel(), packet.GetSequence())
			proof, proofHeight := suite.chainB.QueryProof(packetAckKey)

			ack := ibcmock.MockAcknowledgement
			if tc.changeAcknowledgement {
				ack = ibcmock.MockFailAcknowledgement
			}

			err = suite.chainA.App.GetIBCKeeper().ConnectionKeeper.VerifyPacketAcknowledgement(
				suite.chainA.GetContext(), connection, malleateHeight(proofHeight, tc.heightDiff), proof,
				packet.GetDestPort(), packet.GetDestChannel(), packet.GetSequence(), ack.Acknowledgement(),
			)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

// TestVerifyPacketReceiptAbsence has chainA verify the receipt
// absence on channelB. The channels on chainA and chainB are fully opened and
// a packet is sent from chainA to chainB and not received.
func (suite *KeeperTestSuite) TestVerifyPacketReceiptAbsence() {
	cases := []struct {
		msg            string
		changeClientID bool
		recvAck        bool
		heightDiff     uint64
		delayPeriod    uint64
		expPass        bool
	}{
		{"verification success", false, false, 0, 0, true},
		{"verification success: delay period passed", false, false, 0, uint64(1 * time.Second.Nanoseconds()), true},
		{"delay period has not passed", false, false, 0, uint64(1 * time.Hour.Nanoseconds()), false},
		{"client state not found - changed client ID", true, false, 0, 0, false},
		{"consensus state not found - increased proof height", false, false, 5, 0, false},
		{"verification failed - acknowledgement was received", false, true, 0, 0, false},
	}

	for _, tc := range cases {
		tc := tc

		suite.Run(tc.msg, func() {
			suite.SetupTest() // reset

			path := ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.Setup(path)

			connection := path.EndpointA.GetConnection()
			connection.DelayPeriod = tc.delayPeriod
			if tc.changeClientID {
				connection.ClientId = ibctesting.InvalidID
			}

			// send, only receive if specified
			packet := channeltypes.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, 0)
			err := path.EndpointA.SendPacket(packet)
			suite.Require().NoError(err)

			if tc.recvAck {
				// increment receiving chain's (chainB) time by 2 hour to always pass receive
				suite.coordinator.IncrementTimeBy(time.Hour * 2)
				suite.coordinator.CommitBlock(suite.chainB)

				err = path.EndpointB.RecvPacket(packet)
				suite.Require().NoError(err)
			} else {
				// need to update height to prove absence
				suite.coordinator.CommitBlock(suite.chainA, suite.chainB)
				path.EndpointA.UpdateClient()
			}

			packetReceiptKey := host.PacketReceiptKey(packet.GetDestPort(), packet.GetDestChannel(), packet.GetSequence())
			proof, proofHeight := suite.chainB.QueryProof(packetReceiptKey)

			err = suite.chainA.App.GetIBCKeeper().ConnectionKeeper.VerifyPacketReceiptAbsence(
				suite.chainA.GetContext(), connection, malleateHeight(proofHeight, tc.heightDiff), proof,
				packet.GetDestPort(), packet.GetDestChannel(), packet.GetSequence(),
			)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

// TestVerifyNextSequenceRecv has chainA verify the next sequence receive on
// channelB. The channels on chainA and chainB are fully opened and a packet
// is sent from chainA to chainB and received.
func (suite *KeeperTestSuite) TestVerifyNextSequenceRecv() {
	cases := []struct {
		msg            string
		changeClientID bool
		offsetSeq      uint64
		heightDiff     uint64
		delayPeriod    uint64
		expPass        bool
	}{
		{"verification success", false, 0, 0, 0, true},
		{"verification success: delay period passed", false, 0, 0, uint64(1 * time.Second.Nanoseconds()), true},
		{"delay period has not passed", false, 0, 0, uint64(1 * time.Hour.Nanoseconds()), false},
		{"client state not found- changed client ID", true, 0, 0, 0, false},
		{"consensus state not found - increased proof height", false, 0, 5, 0, false},
		{"verification failed - wrong expected next seq recv", false, 1, 0, 0, false},
	}

	for _, tc := range cases {
		tc := tc

		suite.Run(tc.msg, func() {
			suite.SetupTest() // reset

			path := ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.Setup(path)

			connection := path.EndpointA.GetConnection()
			connection.DelayPeriod = tc.delayPeriod
			if tc.changeClientID {
				connection.ClientId = ibctesting.InvalidID
			}

			// send and receive packet
			packet := channeltypes.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, 0)
			err := path.EndpointA.SendPacket(packet)
			suite.Require().NoError(err)

			// increment receiving chain's (chainB) time by 2 hour to always pass receive
			suite.coordinator.IncrementTimeBy(time.Hour * 2)
			suite.coordinator.CommitBlock(suite.chainB)

			err = path.EndpointB.RecvPacket(packet)
			suite.Require().NoError(err)

			nextSeqRecvKey := host.NextSequenceRecvKey(packet.GetDestPort(), packet.GetDestChannel())
			proof, proofHeight := suite.chainB.QueryProof(nextSeqRecvKey)

			err = suite.chainA.App.GetIBCKeeper().ConnectionKeeper.VerifyNextSequenceRecv(
				suite.chainA.GetContext(), connection, malleateHeight(proofHeight, tc.heightDiff), proof,
				packet.GetDestPort(), packet.GetDestChannel(), packet.GetSequence()+tc.offsetSeq,
			)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func malleateHeight(height exported.Height, diff uint64) exported.Height {
	return clienttypes.NewHeight(height.GetRevisionNumber(), height.GetRevisionHeight()+diff)
}
