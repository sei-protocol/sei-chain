package keeper_test

import (
	"fmt"
	"time"

	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	"github.com/cosmos/ibc-go/v3/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v3/modules/core/24-host"
	"github.com/cosmos/ibc-go/v3/modules/core/exported"
	ibctmtypes "github.com/cosmos/ibc-go/v3/modules/light-clients/07-tendermint/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
	ibcmock "github.com/cosmos/ibc-go/v3/testing/mock"
)

var defaultTimeoutHeight = clienttypes.NewHeight(0, 100000)

// TestVerifyClientState verifies a client state of chainA
// stored on path.EndpointB (which is on chainB)
func (suite *KeeperTestSuite) TestVerifyClientState() {
	var (
		path       *ibctesting.Path
		heightDiff uint64
	)
	cases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{"verification success", func() {}, true},
		{"client state not found", func() {
			connection := path.EndpointA.GetConnection()
			connection.ClientId = ibctesting.InvalidID
			path.EndpointA.SetConnection(connection)
		}, false},
		{"consensus state for proof height not found", func() {
			heightDiff = 5
		}, false},
		{"verification failed", func() {
			counterpartyClient := path.EndpointB.GetClientState().(*ibctmtypes.ClientState)
			counterpartyClient.ChainId = "wrongChainID"
			path.EndpointB.SetClientState(counterpartyClient)
		}, false},
		{"client status is not active - client is expired", func() {
			clientState := path.EndpointA.GetClientState().(*ibctmtypes.ClientState)
			clientState.FrozenHeight = clienttypes.NewHeight(0, 1)
			path.EndpointA.SetClientState(clientState)
		}, false},
	}

	for _, tc := range cases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest() // reset
			heightDiff = 0    // must be explicitly changed

			path = ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)

			tc.malleate()

			counterpartyClient, clientProof := path.EndpointB.QueryClientStateProof()
			proofHeight := clienttypes.NewHeight(0, uint64(suite.chainB.GetContext().BlockHeight()-1))

			connection := path.EndpointA.GetConnection()

			err := suite.chainA.App.GetIBCKeeper().ConnectionKeeper.VerifyClientState(
				suite.chainA.GetContext(), connection,
				malleateHeight(proofHeight, heightDiff), clientProof, counterpartyClient,
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
		path       *ibctesting.Path
		heightDiff uint64
	)
	cases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{"verification success", func() {}, true},
		{"client state not found", func() {
			connection := path.EndpointA.GetConnection()
			connection.ClientId = ibctesting.InvalidID
			path.EndpointA.SetConnection(connection)
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
		{"client status is not active - client is expired", func() {
			clientState := path.EndpointA.GetClientState().(*ibctmtypes.ClientState)
			clientState.FrozenHeight = clienttypes.NewHeight(0, 1)
			path.EndpointA.SetClientState(clientState)
		}, false},
	}

	for _, tc := range cases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest() // reset
			heightDiff = 0    // must be explicitly changed in malleate
			path = ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)

			tc.malleate()

			connection := path.EndpointA.GetConnection()

			proof, consensusHeight := suite.chainB.QueryConsensusStateProof(path.EndpointB.ClientID)
			proofHeight := clienttypes.NewHeight(0, uint64(suite.chainB.GetContext().BlockHeight()-1))
			consensusState, err := suite.chainA.App.GetIBCKeeper().ClientKeeper.GetSelfConsensusState(suite.chainA.GetContext(), consensusHeight)
			suite.Require().NoError(err)

			err = suite.chainA.App.GetIBCKeeper().ConnectionKeeper.VerifyClientConsensusState(
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
	var (
		path       *ibctesting.Path
		heightDiff uint64
	)
	cases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{"verification success", func() {}, true},
		{"client state not found - changed client ID", func() {
			connection := path.EndpointA.GetConnection()
			connection.ClientId = ibctesting.InvalidID
			path.EndpointA.SetConnection(connection)
		}, false},
		{"consensus state not found - increased proof height", func() {
			heightDiff = 5
		}, false},
		{"verification failed - connection state is different than proof", func() {
			connection := path.EndpointA.GetConnection()
			connection.State = types.TRYOPEN
			path.EndpointA.SetConnection(connection)
		}, false},
		{"client status is not active - client is expired", func() {
			clientState := path.EndpointA.GetClientState().(*ibctmtypes.ClientState)
			clientState.FrozenHeight = clienttypes.NewHeight(0, 1)
			path.EndpointA.SetClientState(clientState)
		}, false},
	}

	for _, tc := range cases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			path = ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)

			connectionKey := host.ConnectionKey(path.EndpointB.ConnectionID)
			proof, proofHeight := suite.chainB.QueryProof(connectionKey)

			tc.malleate()

			connection := path.EndpointA.GetConnection()

			expectedConnection := path.EndpointB.GetConnection()

			err := suite.chainA.App.GetIBCKeeper().ConnectionKeeper.VerifyConnectionState(
				suite.chainA.GetContext(), connection,
				malleateHeight(proofHeight, heightDiff), proof, path.EndpointB.ConnectionID, expectedConnection,
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
	var (
		path       *ibctesting.Path
		heightDiff uint64
	)
	cases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{"verification success", func() {}, true},
		{"client state not found- changed client ID", func() {
			connection := path.EndpointA.GetConnection()
			connection.ClientId = ibctesting.InvalidID
			path.EndpointA.SetConnection(connection)
		}, false},
		{"consensus state not found - increased proof height", func() {
			heightDiff = 5
		}, false},
		{"verification failed - changed channel state", func() {
			channel := path.EndpointA.GetChannel()
			channel.State = channeltypes.TRYOPEN
			path.EndpointA.SetChannel(channel)
		}, false},
		{"client status is not active - client is expired", func() {
			clientState := path.EndpointA.GetClientState().(*ibctmtypes.ClientState)
			clientState.FrozenHeight = clienttypes.NewHeight(0, 1)
			path.EndpointA.SetClientState(clientState)
		}, false},
	}

	for _, tc := range cases {
		tc := tc

		suite.Run(fmt.Sprintf("Case %s", tc.name), func() {
			suite.SetupTest() // reset

			path = ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.Setup(path)

			channelKey := host.ChannelKey(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
			proof, proofHeight := suite.chainB.QueryProof(channelKey)

			tc.malleate()
			connection := path.EndpointA.GetConnection()

			channel := path.EndpointB.GetChannel()

			err := suite.chainA.App.GetIBCKeeper().ConnectionKeeper.VerifyChannelState(
				suite.chainA.GetContext(), connection, malleateHeight(proofHeight, heightDiff), proof,
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
	var (
		path            *ibctesting.Path
		packet          channeltypes.Packet
		heightDiff      uint64
		delayTimePeriod uint64
		timePerBlock    uint64
	)
	cases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{"verification success", func() {}, true},
		{"verification success: delay period passed", func() {
			delayTimePeriod = uint64(1 * time.Second.Nanoseconds())
		}, true},
		{"delay time period has not passed", func() {
			delayTimePeriod = uint64(1 * time.Hour.Nanoseconds())
		}, false},
		{"delay block period has not passed", func() {
			// make timePerBlock 1 nanosecond so that block delay is not passed.
			// must also set a non-zero time delay to ensure block delay is enforced.
			delayTimePeriod = uint64(1 * time.Second.Nanoseconds())
			timePerBlock = 1
		}, false},
		{"client state not found- changed client ID", func() {
			connection := path.EndpointB.GetConnection()
			connection.ClientId = ibctesting.InvalidID
			path.EndpointB.SetConnection(connection)
		}, false},
		{"consensus state not found - increased proof height", func() {
			heightDiff = 5
		}, false},
		{"verification failed - changed packet commitment state", func() {
			packet.Data = []byte(ibctesting.InvalidID)
		}, false},
		{"client status is not active - client is expired", func() {
			clientState := path.EndpointB.GetClientState().(*ibctmtypes.ClientState)
			clientState.FrozenHeight = clienttypes.NewHeight(0, 1)
			path.EndpointB.SetClientState(clientState)
		}, false},
	}

	for _, tc := range cases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			path = ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.Setup(path)

			packet = channeltypes.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, 0)
			err := path.EndpointA.SendPacket(packet)
			suite.Require().NoError(err)

			// reset variables
			heightDiff = 0
			delayTimePeriod = 0
			timePerBlock = 0
			tc.malleate()

			connection := path.EndpointB.GetConnection()
			connection.DelayPeriod = delayTimePeriod
			commitmentKey := host.PacketCommitmentKey(packet.GetSourcePort(), packet.GetSourceChannel(), packet.GetSequence())
			proof, proofHeight := suite.chainA.QueryProof(commitmentKey)

			// set time per block param
			if timePerBlock != 0 {
				suite.chainB.App.GetIBCKeeper().ConnectionKeeper.SetParams(suite.chainB.GetContext(), types.NewParams(timePerBlock))
			}

			commitment := channeltypes.CommitPacket(suite.chainB.App.GetIBCKeeper().Codec(), packet)
			err = suite.chainB.App.GetIBCKeeper().ConnectionKeeper.VerifyPacketCommitment(
				suite.chainB.GetContext(), connection, malleateHeight(proofHeight, heightDiff), proof,
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
	var (
		path            *ibctesting.Path
		ack             exported.Acknowledgement
		heightDiff      uint64
		delayTimePeriod uint64
		timePerBlock    uint64
	)

	cases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{"verification success", func() {}, true},
		{"verification success: delay period passed", func() {
			delayTimePeriod = uint64(1 * time.Second.Nanoseconds())
		}, true},
		{"delay time period has not passed", func() {
			delayTimePeriod = uint64(1 * time.Hour.Nanoseconds())
		}, false},
		{"delay block period has not passed", func() {
			// make timePerBlock 1 nanosecond so that block delay is not passed.
			// must also set a non-zero time delay to ensure block delay is enforced.
			delayTimePeriod = uint64(1 * time.Second.Nanoseconds())
			timePerBlock = 1
		}, false},
		{"client state not found- changed client ID", func() {
			connection := path.EndpointA.GetConnection()
			connection.ClientId = ibctesting.InvalidID
			path.EndpointA.SetConnection(connection)
		}, false},
		{"consensus state not found - increased proof height", func() {
			heightDiff = 5
		}, false},
		{"verification failed - changed acknowledgement", func() {
			ack = ibcmock.MockFailAcknowledgement
		}, false},
		{"client status is not active - client is expired", func() {
			clientState := path.EndpointA.GetClientState().(*ibctmtypes.ClientState)
			clientState.FrozenHeight = clienttypes.NewHeight(0, 1)
			path.EndpointA.SetClientState(clientState)
		}, false},
	}

	for _, tc := range cases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest()                 // reset
			ack = ibcmock.MockAcknowledgement // must be explicitly changed

			path = ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.Setup(path)

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

			// reset variables
			heightDiff = 0
			delayTimePeriod = 0
			timePerBlock = 0
			tc.malleate()

			connection := path.EndpointA.GetConnection()
			connection.DelayPeriod = delayTimePeriod

			// set time per block param
			if timePerBlock != 0 {
				suite.chainA.App.GetIBCKeeper().ConnectionKeeper.SetParams(suite.chainA.GetContext(), types.NewParams(timePerBlock))
			}

			err = suite.chainA.App.GetIBCKeeper().ConnectionKeeper.VerifyPacketAcknowledgement(
				suite.chainA.GetContext(), connection, malleateHeight(proofHeight, heightDiff), proof,
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
	var (
		path            *ibctesting.Path
		packet          channeltypes.Packet
		heightDiff      uint64
		delayTimePeriod uint64
		timePerBlock    uint64
	)

	cases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{"verification success", func() {}, true},
		{"verification success: delay period passed", func() {
			delayTimePeriod = uint64(1 * time.Second.Nanoseconds())
		}, true},
		{"delay time period has not passed", func() {
			delayTimePeriod = uint64(1 * time.Hour.Nanoseconds())
		}, false},
		{"delay block period has not passed", func() {
			// make timePerBlock 1 nanosecond so that block delay is not passed.
			// must also set a non-zero time delay to ensure block delay is enforced.
			delayTimePeriod = uint64(1 * time.Second.Nanoseconds())
			timePerBlock = 1
		}, false},
		{"client state not found - changed client ID", func() {
			connection := path.EndpointA.GetConnection()
			connection.ClientId = ibctesting.InvalidID
			path.EndpointA.SetConnection(connection)
		}, false},
		{"consensus state not found - increased proof height", func() {
			heightDiff = 5
		}, false},
		{"verification failed - acknowledgement was received", func() {
			// increment receiving chain's (chainB) time by 2 hour to always pass receive
			suite.coordinator.IncrementTimeBy(time.Hour * 2)
			suite.coordinator.CommitBlock(suite.chainB)

			err := path.EndpointB.RecvPacket(packet)
			suite.Require().NoError(err)
		}, false},
		{"client status is not active - client is expired", func() {
			clientState := path.EndpointA.GetClientState().(*ibctmtypes.ClientState)
			clientState.FrozenHeight = clienttypes.NewHeight(0, 1)
			path.EndpointA.SetClientState(clientState)
		}, false},
	}

	for _, tc := range cases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			path = ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.Setup(path)

			// send, only receive in malleate if applicable
			packet = channeltypes.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, 0)
			err := path.EndpointA.SendPacket(packet)
			suite.Require().NoError(err)

			// reset variables
			heightDiff = 0
			delayTimePeriod = 0
			timePerBlock = 0
			tc.malleate()

			connection := path.EndpointA.GetConnection()
			connection.DelayPeriod = delayTimePeriod

			clientState := path.EndpointA.GetClientState().(*ibctmtypes.ClientState)
			if clientState.FrozenHeight.IsZero() {
				// need to update height to prove absence or receipt
				suite.coordinator.CommitBlock(suite.chainA, suite.chainB)
				path.EndpointA.UpdateClient()
			}

			packetReceiptKey := host.PacketReceiptKey(packet.GetDestPort(), packet.GetDestChannel(), packet.GetSequence())
			proof, proofHeight := suite.chainB.QueryProof(packetReceiptKey)

			// set time per block param
			if timePerBlock != 0 {
				suite.chainA.App.GetIBCKeeper().ConnectionKeeper.SetParams(suite.chainA.GetContext(), types.NewParams(timePerBlock))
			}

			err = suite.chainA.App.GetIBCKeeper().ConnectionKeeper.VerifyPacketReceiptAbsence(
				suite.chainA.GetContext(), connection, malleateHeight(proofHeight, heightDiff), proof,
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
	var (
		path            *ibctesting.Path
		heightDiff      uint64
		delayTimePeriod uint64
		timePerBlock    uint64
		offsetSeq       uint64
	)

	cases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{"verification success", func() {}, true},
		{"verification success: delay period passed", func() {
			delayTimePeriod = uint64(1 * time.Second.Nanoseconds())
		}, true},
		{"delay time period has not passed", func() {
			delayTimePeriod = uint64(1 * time.Hour.Nanoseconds())
		}, false},
		{"delay block period has not passed", func() {
			// make timePerBlock 1 nanosecond so that block delay is not passed.
			// must also set a non-zero time delay to ensure block delay is enforced.
			delayTimePeriod = uint64(1 * time.Second.Nanoseconds())
			timePerBlock = 1
		}, false},
		{"client state not found- changed client ID", func() {
			connection := path.EndpointA.GetConnection()
			connection.ClientId = ibctesting.InvalidID
			path.EndpointA.SetConnection(connection)
		}, false},
		{"consensus state not found - increased proof height", func() {
			heightDiff = 5
		}, false},
		{"verification failed - wrong expected next seq recv", func() {
			offsetSeq = 1
		}, false},
		{"client status is not active - client is expired", func() {
			clientState := path.EndpointA.GetClientState().(*ibctmtypes.ClientState)
			clientState.FrozenHeight = clienttypes.NewHeight(0, 1)
			path.EndpointA.SetClientState(clientState)
		}, false},
	}

	for _, tc := range cases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			path = ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.Setup(path)

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

			// reset variables
			heightDiff = 0
			delayTimePeriod = 0
			timePerBlock = 0
			tc.malleate()

			// set time per block param
			if timePerBlock != 0 {
				suite.chainA.App.GetIBCKeeper().ConnectionKeeper.SetParams(suite.chainA.GetContext(), types.NewParams(timePerBlock))
			}

			connection := path.EndpointA.GetConnection()
			connection.DelayPeriod = delayTimePeriod
			err = suite.chainA.App.GetIBCKeeper().ConnectionKeeper.VerifyNextSequenceRecv(
				suite.chainA.GetContext(), connection, malleateHeight(proofHeight, heightDiff), proof,
				packet.GetDestPort(), packet.GetDestChannel(), packet.GetSequence()+offsetSeq,
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
