package keeper_test

import (
	"errors"
	"fmt"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"

	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	connectiontypes "github.com/cosmos/ibc-go/v3/modules/core/03-connection/types"
	"github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v3/modules/core/24-host"
	"github.com/cosmos/ibc-go/v3/modules/core/exported"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
)

// TestTimeoutPacket test the TimeoutPacket call on chainA by ensuring the timeout has passed
// on chainB, but that no ack has been written yet. Test cases expected to reach proof
// verification must specify which proof to use using the ordered bool.
func (suite *KeeperTestSuite) TestTimeoutPacket() {
	var (
		path        *ibctesting.Path
		packet      types.Packet
		nextSeqRecv uint64
		ordered     bool
		expError    *sdkerrors.Error
	)

	testCases := []testCase{
		{"success: ORDERED", func() {
			ordered = true
			path.SetChannelOrdered()

			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.GetSelfHeight(suite.chainB.GetContext()), uint64(suite.chainB.GetContext().BlockTime().UnixNano()))
			path.EndpointA.SendPacket(packet)
			// need to update chainA's client representing chainB to prove missing ack
			path.EndpointA.UpdateClient()
		}, true},
		{"success: UNORDERED", func() {
			ordered = false

			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.GetSelfHeight(suite.chainB.GetContext()), disabledTimeoutTimestamp)
			path.EndpointA.SendPacket(packet)
			// need to update chainA's client representing chainB to prove missing ack
			path.EndpointA.UpdateClient()
		}, true},
		{"packet already timed out: ORDERED", func() {
			expError = types.ErrNoOpMsg
			ordered = true
			path.SetChannelOrdered()

			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.GetSelfHeight(suite.chainB.GetContext()), uint64(suite.chainB.GetContext().BlockTime().UnixNano()))
			path.EndpointA.SendPacket(packet)
			// need to update chainA's client representing chainB to prove missing ack
			path.EndpointA.UpdateClient()

			err := path.EndpointA.TimeoutPacket(packet)
			suite.Require().NoError(err)
		}, false},
		{"packet already timed out: UNORDERED", func() {
			expError = types.ErrNoOpMsg
			ordered = false

			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.GetSelfHeight(suite.chainB.GetContext()), disabledTimeoutTimestamp)
			path.EndpointA.SendPacket(packet)
			// need to update chainA's client representing chainB to prove missing ack
			path.EndpointA.UpdateClient()

			err := path.EndpointA.TimeoutPacket(packet)
			suite.Require().NoError(err)
		}, false},
		{"channel not found", func() {
			expError = types.ErrChannelNotFound
			// use wrong channel naming
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, ibctesting.InvalidID, ibctesting.InvalidID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
		}, false},
		{"channel not open", func() {
			expError = types.ErrInvalidChannelState
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, path.EndpointA.GetClientState().GetLatestHeight().Increment().(clienttypes.Height), disabledTimeoutTimestamp)
			err := path.EndpointA.SendPacket(packet)
			suite.Require().NoError(err)
			// need to update chainA's client representing chainB to prove missing ack
			path.EndpointA.UpdateClient()

			err = path.EndpointA.SetChannelClosed()
			suite.Require().NoError(err)
		}, false},
		{"packet destination port ≠ channel counterparty port", func() {
			expError = types.ErrInvalidPacket
			suite.coordinator.Setup(path)
			// use wrong port for dest
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, ibctesting.InvalidID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
		}, false},
		{"packet destination channel ID ≠ channel counterparty channel ID", func() {
			expError = types.ErrInvalidPacket
			suite.coordinator.Setup(path)
			// use wrong channel for dest
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, ibctesting.InvalidID, timeoutHeight, disabledTimeoutTimestamp)
		}, false},
		{"connection not found", func() {
			expError = connectiontypes.ErrConnectionNotFound
			// pass channel check
			suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetChannel(
				suite.chainA.GetContext(),
				path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID,
				types.NewChannel(types.OPEN, types.ORDERED, types.NewCounterparty(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID), []string{connIDA}, path.EndpointA.ChannelConfig.Version),
			)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
		}, false},
		{"timeout", func() {
			expError = types.ErrPacketTimeout
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			path.EndpointA.SendPacket(packet)
			path.EndpointA.UpdateClient()
		}, false},
		{"packet already received ", func() {
			expError = types.ErrPacketReceived
			ordered = true
			path.SetChannelOrdered()

			nextSeqRecv = 2

			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, uint64(suite.chainB.GetContext().BlockTime().UnixNano()))
			path.EndpointA.SendPacket(packet)
			path.EndpointA.UpdateClient()
		}, false},
		{"packet hasn't been sent", func() {
			expError = types.ErrNoOpMsg
			ordered = true
			path.SetChannelOrdered()

			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, uint64(suite.chainB.GetContext().BlockTime().UnixNano()))
			path.EndpointA.UpdateClient()
		}, false},
		{"next seq receive verification failed", func() {
			// skip error check, error occurs in light-clients

			// set ordered to false resulting in wrong proof provided
			ordered = false

			path.SetChannelOrdered()

			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.GetSelfHeight(suite.chainB.GetContext()), disabledTimeoutTimestamp)
			path.EndpointA.SendPacket(packet)
			path.EndpointA.UpdateClient()
		}, false},
		{"packet ack verification failed", func() {
			// skip error check, error occurs in light-clients

			// set ordered to true resulting in wrong proof provided
			ordered = true

			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.GetSelfHeight(suite.chainB.GetContext()), disabledTimeoutTimestamp)
			path.EndpointA.SendPacket(packet)
			path.EndpointA.UpdateClient()
		}, false},
	}

	for i, tc := range testCases {
		tc := tc
		suite.Run(fmt.Sprintf("Case %s, %d/%d tests", tc.msg, i, len(testCases)), func() {
			var (
				proof       []byte
				proofHeight exported.Height
			)

			suite.SetupTest() // reset
			expError = nil    // must be expliticly changed by failed cases
			nextSeqRecv = 1   // must be explicitly changed
			path = ibctesting.NewPath(suite.chainA, suite.chainB)

			tc.malleate()

			orderedPacketKey := host.NextSequenceRecvKey(packet.GetDestPort(), packet.GetDestChannel())
			unorderedPacketKey := host.PacketReceiptKey(packet.GetDestPort(), packet.GetDestChannel(), packet.GetSequence())

			if path.EndpointB.ConnectionID != "" {
				if ordered {
					proof, proofHeight = path.EndpointB.QueryProof(orderedPacketKey)
				} else {
					proof, proofHeight = path.EndpointB.QueryProof(unorderedPacketKey)
				}
			}

			err := suite.chainA.App.GetIBCKeeper().ChannelKeeper.TimeoutPacket(suite.chainA.GetContext(), packet, proof, proofHeight, nextSeqRecv)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
				// only check if expError is set, since not all error codes can be known
				if expError != nil {
					suite.Require().True(errors.Is(err, expError))
				}

			}
		})
	}
}

// TestTimeoutExectued verifies that packet commitments are deleted on chainA after the
// channel capabilities are verified.
func (suite *KeeperTestSuite) TestTimeoutExecuted() {
	var (
		path    *ibctesting.Path
		packet  types.Packet
		chanCap *capabilitytypes.Capability
	)

	testCases := []testCase{
		{"success ORDERED", func() {
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)

			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.GetSelfHeight(suite.chainB.GetContext()), uint64(suite.chainB.GetContext().BlockTime().UnixNano()))
			path.EndpointA.SendPacket(packet)

			chanCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, true},
		{"channel not found", func() {
			// use wrong channel naming
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, ibctesting.InvalidID, ibctesting.InvalidID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
		}, false},
		{"incorrect capability ORDERED", func() {
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)

			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.GetSelfHeight(suite.chainB.GetContext()), uint64(suite.chainB.GetContext().BlockTime().UnixNano()))
			path.EndpointA.SendPacket(packet)

			chanCap = capabilitytypes.NewCapability(100)
		}, false},
	}

	for i, tc := range testCases {
		tc := tc
		suite.Run(fmt.Sprintf("Case %s, %d/%d tests", tc.msg, i, len(testCases)), func() {
			suite.SetupTest() // reset
			path = ibctesting.NewPath(suite.chainA, suite.chainB)

			tc.malleate()

			err := suite.chainA.App.GetIBCKeeper().ChannelKeeper.TimeoutExecuted(suite.chainA.GetContext(), chanCap, packet)
			pc := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetPacketCommitment(suite.chainA.GetContext(), packet.GetSourcePort(), packet.GetSourceChannel(), packet.GetSequence())

			if tc.expPass {
				suite.NoError(err)
				suite.Nil(pc)
			} else {
				suite.Error(err)
			}
		})
	}
}

// TestTimeoutOnClose tests the call TimeoutOnClose on chainA by closing the corresponding
// channel on chainB after the packet commitment has been created.
func (suite *KeeperTestSuite) TestTimeoutOnClose() {
	var (
		path        *ibctesting.Path
		packet      types.Packet
		chanCap     *capabilitytypes.Capability
		nextSeqRecv uint64
		ordered     bool
	)

	testCases := []testCase{
		{"success: ORDERED", func() {
			ordered = true
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)

			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.GetSelfHeight(suite.chainB.GetContext()), uint64(suite.chainB.GetContext().BlockTime().UnixNano()))
			path.EndpointA.SendPacket(packet)
			path.EndpointB.SetChannelClosed()
			// need to update chainA's client representing chainB to prove missing ack
			path.EndpointA.UpdateClient()

			chanCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, true},
		{"success: UNORDERED", func() {
			ordered = false
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.GetSelfHeight(suite.chainB.GetContext()), disabledTimeoutTimestamp)
			path.EndpointA.SendPacket(packet)
			path.EndpointB.SetChannelClosed()
			// need to update chainA's client representing chainB to prove missing ack
			path.EndpointA.UpdateClient()

			chanCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, true},
		{"channel not found", func() {
			// use wrong channel naming
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, ibctesting.InvalidID, ibctesting.InvalidID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
		}, false},
		{"packet dest port ≠ channel counterparty port", func() {
			suite.coordinator.Setup(path)
			// use wrong port for dest
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, ibctesting.InvalidID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)
			chanCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"packet dest channel ID ≠ channel counterparty channel ID", func() {
			suite.coordinator.Setup(path)
			// use wrong channel for dest
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, ibctesting.InvalidID, timeoutHeight, disabledTimeoutTimestamp)
			chanCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"connection not found", func() {
			// pass channel check
			suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetChannel(
				suite.chainA.GetContext(),
				path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID,
				types.NewChannel(types.OPEN, types.ORDERED, types.NewCounterparty(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID), []string{connIDA}, path.EndpointA.ChannelConfig.Version),
			)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, timeoutHeight, disabledTimeoutTimestamp)

			// create chancap
			suite.chainA.CreateChannelCapability(suite.chainA.GetSimApp().ScopedIBCMockKeeper, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			chanCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"packet hasn't been sent ORDERED", func() {
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)

			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.GetSelfHeight(suite.chainB.GetContext()), uint64(suite.chainB.GetContext().BlockTime().UnixNano()))
			chanCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"packet already received ORDERED", func() {
			path.SetChannelOrdered()
			nextSeqRecv = 2
			ordered = true
			suite.coordinator.Setup(path)

			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.GetSelfHeight(suite.chainB.GetContext()), uint64(suite.chainB.GetContext().BlockTime().UnixNano()))
			path.EndpointA.SendPacket(packet)
			path.EndpointB.SetChannelClosed()
			// need to update chainA's client representing chainB to prove missing ack
			path.EndpointA.UpdateClient()

			chanCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"channel verification failed ORDERED", func() {
			ordered = true
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.GetSelfHeight(suite.chainB.GetContext()), uint64(suite.chainB.GetContext().BlockTime().UnixNano()))
			path.EndpointA.SendPacket(packet)
			chanCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"next seq receive verification failed ORDERED", func() {
			// set ordered to false providing the wrong proof for ORDERED case
			ordered = false

			path.SetChannelOrdered()
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.GetSelfHeight(suite.chainB.GetContext()), uint64(suite.chainB.GetContext().BlockTime().UnixNano()))
			path.EndpointA.SendPacket(packet)
			path.EndpointB.SetChannelClosed()
			path.EndpointA.UpdateClient()
			chanCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"packet ack verification failed", func() {
			// set ordered to true providing the wrong proof for UNORDERED case
			ordered = true
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.GetSelfHeight(suite.chainB.GetContext()), disabledTimeoutTimestamp)
			path.EndpointA.SendPacket(packet)
			path.EndpointB.SetChannelClosed()
			path.EndpointA.UpdateClient()
			chanCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"channel capability not found ORDERED", func() {
			ordered = true
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)

			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.GetSelfHeight(suite.chainB.GetContext()), uint64(suite.chainB.GetContext().BlockTime().UnixNano()))
			path.EndpointA.SendPacket(packet)
			path.EndpointB.SetChannelClosed()
			// need to update chainA's client representing chainB to prove missing ack
			path.EndpointA.UpdateClient()

			chanCap = capabilitytypes.NewCapability(100)
		}, false},
	}

	for i, tc := range testCases {
		tc := tc
		suite.Run(fmt.Sprintf("Case %s, %d/%d tests", tc.msg, i, len(testCases)), func() {
			var proof []byte

			suite.SetupTest() // reset
			nextSeqRecv = 1   // must be explicitly changed
			path = ibctesting.NewPath(suite.chainA, suite.chainB)

			tc.malleate()

			channelKey := host.ChannelKey(packet.GetDestPort(), packet.GetDestChannel())
			unorderedPacketKey := host.PacketReceiptKey(packet.GetDestPort(), packet.GetDestChannel(), packet.GetSequence())
			orderedPacketKey := host.NextSequenceRecvKey(packet.GetDestPort(), packet.GetDestChannel())

			proofClosed, proofHeight := suite.chainB.QueryProof(channelKey)

			if ordered {
				proof, _ = suite.chainB.QueryProof(orderedPacketKey)
			} else {
				proof, _ = suite.chainB.QueryProof(unorderedPacketKey)
			}

			err := suite.chainA.App.GetIBCKeeper().ChannelKeeper.TimeoutOnClose(suite.chainA.GetContext(), chanCap, packet, proof, proofClosed, proofHeight, nextSeqRecv)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}

}
