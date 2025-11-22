package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
)

// KeeperTestSuite is a testing suite to test keeper functions.
type KeeperTestSuite struct {
	suite.Suite

	coordinator *ibctesting.Coordinator

	// testing chains used for convenience and readability
	chainA *ibctesting.TestChain
	chainB *ibctesting.TestChain
}

// TestKeeperTestSuite runs all the tests within this package.
func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

// SetupTest creates a coordinator with 2 test chains.
func (suite *KeeperTestSuite) SetupTest() {
	suite.coordinator = ibctesting.NewCoordinator(suite.T(), 2)
	suite.chainA = suite.coordinator.GetChain(ibctesting.GetChainID(1))
	suite.chainB = suite.coordinator.GetChain(ibctesting.GetChainID(2))
	// commit some blocks so that QueryProof returns valid proof (cannot return valid query if height <= 1)
	suite.coordinator.CommitNBlocks(suite.chainA, 2)
	suite.coordinator.CommitNBlocks(suite.chainB, 2)
}

// TestSetChannel create clients and connections on both chains. It tests for the non-existence
// and existence of a channel in INIT on chainA.
func (suite *KeeperTestSuite) TestSetChannel() {
	// create client and connections on both chains
	path := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.SetupConnections(path)

	// check for channel to be created on chainA
	_, found := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetChannel(suite.chainA.GetContext(), path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
	suite.False(found)

	path.SetChannelOrdered()

	// init channel
	err := path.EndpointA.ChanOpenInit()
	suite.NoError(err)

	storedChannel, found := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetChannel(suite.chainA.GetContext(), path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
	// counterparty channel id is empty after open init
	expectedCounterparty := types.NewCounterparty(path.EndpointB.ChannelConfig.PortID, "")

	suite.True(found)
	suite.Equal(types.INIT, storedChannel.State)
	suite.Equal(types.ORDERED, storedChannel.Ordering)
	suite.Equal(expectedCounterparty, storedChannel.Counterparty)
}

// TestGetAllChannels creates multiple channels on chain A through various connections
// and tests their retrieval. 2 channels are on connA0 and 1 channel is on connA1
func (suite KeeperTestSuite) TestGetAllChannels() {
	path := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.Setup(path)
	// channel0 on first connection on chainA
	counterparty0 := types.Counterparty{
		PortId:    path.EndpointB.ChannelConfig.PortID,
		ChannelId: path.EndpointB.ChannelID,
	}

	// path1 creates a second channel on first connection on chainA
	path1 := ibctesting.NewPath(suite.chainA, suite.chainB)
	path1.SetChannelOrdered()
	path1.EndpointA.ClientID = path.EndpointA.ClientID
	path1.EndpointB.ClientID = path.EndpointB.ClientID
	path1.EndpointA.ConnectionID = path.EndpointA.ConnectionID
	path1.EndpointB.ConnectionID = path.EndpointB.ConnectionID

	suite.coordinator.CreateMockChannels(path1)
	counterparty1 := types.Counterparty{
		PortId:    path1.EndpointB.ChannelConfig.PortID,
		ChannelId: path1.EndpointB.ChannelID,
	}

	path2 := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.SetupConnections(path2)

	// path2 creates a second channel on chainA
	err := path2.EndpointA.ChanOpenInit()
	suite.Require().NoError(err)

	// counterparty channel id is empty after open init
	counterparty2 := types.Counterparty{
		PortId:    path2.EndpointB.ChannelConfig.PortID,
		ChannelId: "",
	}

	channel0 := types.NewChannel(
		types.OPEN, types.UNORDERED,
		counterparty0, []string{path.EndpointA.ConnectionID}, path.EndpointA.ChannelConfig.Version,
	)
	channel1 := types.NewChannel(
		types.OPEN, types.ORDERED,
		counterparty1, []string{path1.EndpointA.ConnectionID}, path1.EndpointA.ChannelConfig.Version,
	)
	channel2 := types.NewChannel(
		types.INIT, types.UNORDERED,
		counterparty2, []string{path2.EndpointA.ConnectionID}, path2.EndpointA.ChannelConfig.Version,
	)

	expChannels := []types.IdentifiedChannel{
		types.NewIdentifiedChannel(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, channel0),
		types.NewIdentifiedChannel(path1.EndpointA.ChannelConfig.PortID, path1.EndpointA.ChannelID, channel1),
		types.NewIdentifiedChannel(path2.EndpointA.ChannelConfig.PortID, path2.EndpointA.ChannelID, channel2),
	}

	ctxA := suite.chainA.GetContext()

	channels := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetAllChannels(ctxA)
	suite.Require().Len(channels, len(expChannels))
	suite.Require().Equal(expChannels, channels)
}

// TestGetAllSequences sets all packet sequences for two different channels on chain A and
// tests their retrieval.
func (suite KeeperTestSuite) TestGetAllSequences() {
	path := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.Setup(path)

	path1 := ibctesting.NewPath(suite.chainA, suite.chainB)
	path1.SetChannelOrdered()
	path1.EndpointA.ClientID = path.EndpointA.ClientID
	path1.EndpointB.ClientID = path.EndpointB.ClientID
	path1.EndpointA.ConnectionID = path.EndpointA.ConnectionID
	path1.EndpointB.ConnectionID = path.EndpointB.ConnectionID

	suite.coordinator.CreateMockChannels(path1)

	seq1 := types.NewPacketSequence(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, 1)
	seq2 := types.NewPacketSequence(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, 2)
	seq3 := types.NewPacketSequence(path1.EndpointA.ChannelConfig.PortID, path1.EndpointA.ChannelID, 3)

	// seq1 should be overwritten by seq2
	expSeqs := []types.PacketSequence{seq2, seq3}

	ctxA := suite.chainA.GetContext()

	for _, seq := range []types.PacketSequence{seq1, seq2, seq3} {
		suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetNextSequenceSend(ctxA, seq.PortId, seq.ChannelId, seq.Sequence)
		suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetNextSequenceRecv(ctxA, seq.PortId, seq.ChannelId, seq.Sequence)
		suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetNextSequenceAck(ctxA, seq.PortId, seq.ChannelId, seq.Sequence)
	}

	sendSeqs := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetAllPacketSendSeqs(ctxA)
	recvSeqs := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetAllPacketRecvSeqs(ctxA)
	ackSeqs := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetAllPacketAckSeqs(ctxA)
	suite.Len(sendSeqs, 2)
	suite.Len(recvSeqs, 2)
	suite.Len(ackSeqs, 2)

	suite.Equal(expSeqs, sendSeqs)
	suite.Equal(expSeqs, recvSeqs)
	suite.Equal(expSeqs, ackSeqs)
}

// TestGetAllPacketState creates a set of acks, packet commitments, and receipts on two different
// channels on chain A and tests their retrieval.
func (suite KeeperTestSuite) TestGetAllPacketState() {
	path := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.Setup(path)

	path1 := ibctesting.NewPath(suite.chainA, suite.chainB)
	path1.EndpointA.ClientID = path.EndpointA.ClientID
	path1.EndpointB.ClientID = path.EndpointB.ClientID
	path1.EndpointA.ConnectionID = path.EndpointA.ConnectionID
	path1.EndpointB.ConnectionID = path.EndpointB.ConnectionID

	suite.coordinator.CreateMockChannels(path1)

	// channel 0 acks
	ack1 := types.NewPacketState(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, 1, []byte("ack"))
	ack2 := types.NewPacketState(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, 2, []byte("ack"))

	// duplicate ack
	ack2dup := types.NewPacketState(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, 2, []byte("ack"))

	// channel 1 acks
	ack3 := types.NewPacketState(path1.EndpointA.ChannelConfig.PortID, path1.EndpointA.ChannelID, 1, []byte("ack"))

	// create channel 0 receipts
	receipt := string([]byte{byte(1)})
	rec1 := types.NewPacketState(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, 1, []byte(receipt))
	rec2 := types.NewPacketState(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, 2, []byte(receipt))

	// channel 1 receipts
	rec3 := types.NewPacketState(path1.EndpointA.ChannelConfig.PortID, path1.EndpointA.ChannelID, 1, []byte(receipt))
	rec4 := types.NewPacketState(path1.EndpointA.ChannelConfig.PortID, path1.EndpointA.ChannelID, 2, []byte(receipt))

	// channel 0 packet commitments
	comm1 := types.NewPacketState(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, 1, []byte("hash"))
	comm2 := types.NewPacketState(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, 2, []byte("hash"))

	// channel 1 packet commitments
	comm3 := types.NewPacketState(path1.EndpointA.ChannelConfig.PortID, path1.EndpointA.ChannelID, 1, []byte("hash"))
	comm4 := types.NewPacketState(path1.EndpointA.ChannelConfig.PortID, path1.EndpointA.ChannelID, 2, []byte("hash"))

	expAcks := []types.PacketState{ack1, ack2, ack3}
	expReceipts := []types.PacketState{rec1, rec2, rec3, rec4}
	expCommitments := []types.PacketState{comm1, comm2, comm3, comm4}

	ctxA := suite.chainA.GetContext()

	// set acknowledgements
	for _, ack := range []types.PacketState{ack1, ack2, ack2dup, ack3} {
		suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetPacketAcknowledgement(ctxA, ack.PortId, ack.ChannelId, ack.Sequence, ack.Data)
	}

	// set packet receipts
	for _, rec := range expReceipts {
		suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetPacketReceipt(ctxA, rec.PortId, rec.ChannelId, rec.Sequence)
	}

	// set packet commitments
	for _, comm := range expCommitments {
		suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetPacketCommitment(ctxA, comm.PortId, comm.ChannelId, comm.Sequence, comm.Data)
	}

	acks := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetAllPacketAcks(ctxA)
	receipts := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetAllPacketReceipts(ctxA)
	commitments := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetAllPacketCommitments(ctxA)

	suite.Require().Len(acks, len(expAcks))
	suite.Require().Len(commitments, len(expCommitments))
	suite.Require().Len(receipts, len(expReceipts))

	suite.Require().Equal(expAcks, acks)
	suite.Require().Equal(expReceipts, receipts)
	suite.Require().Equal(expCommitments, commitments)
}

// TestSetSequence verifies that the keeper correctly sets the sequence counters.
func (suite *KeeperTestSuite) TestSetSequence() {
	path := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.Setup(path)

	ctxA := suite.chainA.GetContext()
	one := uint64(1)

	// initialized channel has next send seq of 1
	seq, found := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetNextSequenceSend(ctxA, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
	suite.True(found)
	suite.Equal(one, seq)

	// initialized channel has next seq recv of 1
	seq, found = suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetNextSequenceRecv(ctxA, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
	suite.True(found)
	suite.Equal(one, seq)

	// initialized channel has next seq ack of
	seq, found = suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetNextSequenceAck(ctxA, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
	suite.True(found)
	suite.Equal(one, seq)

	nextSeqSend, nextSeqRecv, nextSeqAck := uint64(10), uint64(10), uint64(10)
	suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetNextSequenceSend(ctxA, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, nextSeqSend)
	suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetNextSequenceRecv(ctxA, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, nextSeqRecv)
	suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetNextSequenceAck(ctxA, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, nextSeqAck)

	storedNextSeqSend, found := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetNextSequenceSend(ctxA, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
	suite.True(found)
	suite.Equal(nextSeqSend, storedNextSeqSend)

	storedNextSeqRecv, found := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetNextSequenceSend(ctxA, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
	suite.True(found)
	suite.Equal(nextSeqRecv, storedNextSeqRecv)

	storedNextSeqAck, found := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetNextSequenceAck(ctxA, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
	suite.True(found)
	suite.Equal(nextSeqAck, storedNextSeqAck)
}

// TestGetAllPacketCommitmentsAtChannel verifies that the keeper returns all stored packet
// commitments for a specific channel. The test will store consecutive commitments up to the
// value of "seq" and then add non-consecutive up to the value of "maxSeq". A final commitment
// with the value maxSeq + 1 is set on a different channel.
func (suite *KeeperTestSuite) TestGetAllPacketCommitmentsAtChannel() {
	path := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.Setup(path)

	// create second channel
	path1 := ibctesting.NewPath(suite.chainA, suite.chainB)
	path1.SetChannelOrdered()
	path1.EndpointA.ClientID = path.EndpointA.ClientID
	path1.EndpointB.ClientID = path.EndpointB.ClientID
	path1.EndpointA.ConnectionID = path.EndpointA.ConnectionID
	path1.EndpointB.ConnectionID = path.EndpointB.ConnectionID

	suite.coordinator.CreateMockChannels(path1)

	ctxA := suite.chainA.GetContext()
	expectedSeqs := make(map[uint64]bool)
	hash := []byte("commitment")

	seq := uint64(15)
	maxSeq := uint64(25)
	suite.Require().Greater(maxSeq, seq)

	// create consecutive commitments
	for i := uint64(1); i < seq; i++ {
		suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetPacketCommitment(ctxA, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, i, hash)
		expectedSeqs[i] = true
	}

	// add non-consecutive commitments
	for i := seq; i < maxSeq; i += 2 {
		suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetPacketCommitment(ctxA, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, i, hash)
		expectedSeqs[i] = true
	}

	// add sequence on different channel/port
	suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetPacketCommitment(ctxA, path1.EndpointA.ChannelConfig.PortID, path1.EndpointA.ChannelID, maxSeq+1, hash)

	commitments := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetAllPacketCommitmentsAtChannel(ctxA, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)

	suite.Equal(len(expectedSeqs), len(commitments))
	// ensure above for loops occurred
	suite.NotEqual(0, len(commitments))

	// verify that all the packet commitments were stored
	for _, packet := range commitments {
		suite.True(expectedSeqs[packet.Sequence])
		suite.Equal(path.EndpointA.ChannelConfig.PortID, packet.PortId)
		suite.Equal(path.EndpointA.ChannelID, packet.ChannelId)
		suite.Equal(hash, packet.Data)

		// prevent duplicates from passing checks
		expectedSeqs[packet.Sequence] = false
	}
}

// TestSetPacketAcknowledgement verifies that packet acknowledgements are correctly
// set in the keeper.
func (suite *KeeperTestSuite) TestSetPacketAcknowledgement() {
	path := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.Setup(path)

	ctxA := suite.chainA.GetContext()
	seq := uint64(10)

	storedAckHash, found := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetPacketAcknowledgement(ctxA, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, seq)
	suite.Require().False(found)
	suite.Require().Nil(storedAckHash)

	ackHash := []byte("ackhash")
	suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetPacketAcknowledgement(ctxA, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, seq, ackHash)

	storedAckHash, found = suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetPacketAcknowledgement(ctxA, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, seq)
	suite.Require().True(found)
	suite.Require().Equal(ackHash, storedAckHash)
	suite.Require().True(suite.chainA.App.GetIBCKeeper().ChannelKeeper.HasPacketAcknowledgement(ctxA, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, seq))
}
