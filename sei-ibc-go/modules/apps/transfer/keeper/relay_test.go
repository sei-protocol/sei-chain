package keeper_test

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v3/modules/core/24-host"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
	"github.com/cosmos/ibc-go/v3/testing/simapp"
)

// test sending from chainA to chainB using both coin that orignate on
// chainA and coin that orignate on chainB
func (suite *KeeperTestSuite) TestSendTransfer() {
	var (
		amount sdk.Coin
		path   *ibctesting.Path
		err    error
	)

	testCases := []struct {
		msg            string
		malleate       func()
		sendFromSource bool
		expPass        bool
	}{
		{"successful transfer from source chain",
			func() {
				suite.coordinator.CreateTransferChannels(path)
				amount = sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(100))
			}, true, true},
		{"successful transfer with coin from counterparty chain",
			func() {
				// send coin from chainA back to chainB
				suite.coordinator.CreateTransferChannels(path)
				amount = types.GetTransferCoin(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, sdk.DefaultBondDenom, sdk.NewInt(100))
			}, false, true},
		{"source channel not found",
			func() {
				// channel references wrong ID
				suite.coordinator.CreateTransferChannels(path)
				path.EndpointA.ChannelID = ibctesting.InvalidID
				amount = sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(100))
			}, true, false},
		{"next seq send not found",
			func() {
				path.EndpointA.ChannelID = "channel-0"
				path.EndpointB.ChannelID = "channel-0"
				// manually create channel so next seq send is never set
				suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetChannel(
					suite.chainA.GetContext(),
					path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID,
					channeltypes.NewChannel(channeltypes.OPEN, channeltypes.ORDERED, channeltypes.NewCounterparty(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID), []string{path.EndpointA.ConnectionID}, ibctesting.DefaultChannelVersion),
				)
				suite.chainA.CreateChannelCapability(suite.chainA.GetSimApp().ScopedIBCMockKeeper, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
				amount = sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(100))
			}, true, false},

		// createOutgoingPacket tests
		// - source chain
		{"send coin failed",
			func() {
				suite.coordinator.CreateTransferChannels(path)
				amount = sdk.NewCoin("randomdenom", sdk.NewInt(100))
			}, true, false},
		// - receiving chain
		{"send from module account failed",
			func() {
				suite.coordinator.CreateTransferChannels(path)
				amount = types.GetTransferCoin(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, " randomdenom", sdk.NewInt(100))
			}, false, false},
		{"channel capability not found",
			func() {
				suite.coordinator.CreateTransferChannels(path)
				cap := suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)

				// Release channel capability
				suite.chainA.GetSimApp().ScopedTransferKeeper.ReleaseCapability(suite.chainA.GetContext(), cap)
				amount = sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(100))
			}, true, false},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(fmt.Sprintf("Case %s", tc.msg), func() {
			suite.SetupTest() // reset
			path = NewTransferPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)

			tc.malleate()

			if !tc.sendFromSource {
				// send coin from chainB to chainA
				coinFromBToA := sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(100))
				transferMsg := types.NewMsgTransfer(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, coinFromBToA, suite.chainB.SenderAccount.GetAddress().String(), suite.chainA.SenderAccount.GetAddress().String(), clienttypes.NewHeight(0, 110), 0)
				_, err = suite.chainB.SendMsgs(transferMsg)
				suite.Require().NoError(err) // message committed

				// receive coin on chainA from chainB
				fungibleTokenPacket := types.NewFungibleTokenPacketData(coinFromBToA.Denom, coinFromBToA.Amount.String(), suite.chainB.SenderAccount.GetAddress().String(), suite.chainA.SenderAccount.GetAddress().String())
				packet := channeltypes.NewPacket(fungibleTokenPacket.GetBytes(), 1, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, clienttypes.NewHeight(0, 110), 0)

				// get proof of packet commitment from chainB
				err = path.EndpointA.UpdateClient()
				suite.Require().NoError(err)
				packetKey := host.PacketCommitmentKey(packet.GetSourcePort(), packet.GetSourceChannel(), packet.GetSequence())
				proof, proofHeight := path.EndpointB.QueryProof(packetKey)

				recvMsg := channeltypes.NewMsgRecvPacket(packet, proof, proofHeight, suite.chainA.SenderAccount.GetAddress().String())
				_, err = suite.chainA.SendMsgs(recvMsg)
				suite.Require().NoError(err) // message committed
			}

			err = suite.chainA.GetSimApp().TransferKeeper.SendTransfer(
				suite.chainA.GetContext(), path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, amount,
				suite.chainA.SenderAccount.GetAddress(), suite.chainB.SenderAccount.GetAddress().String(), clienttypes.NewHeight(0, 110), 0,
			)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

// test receiving coin on chainB with coin that orignate on chainA and
// coin that orignated on chainB (source). The bulk of the testing occurs
// in the test case for loop since setup is intensive for all cases. The
// malleate function allows for testing invalid cases.
func (suite *KeeperTestSuite) TestOnRecvPacket() {
	var (
		trace    types.DenomTrace
		amount   sdk.Int
		receiver string
	)

	testCases := []struct {
		msg          string
		malleate     func()
		recvIsSource bool // the receiving chain is the source of the coin originally
		expPass      bool
	}{
		{"success receive on source chain", func() {}, true, true},
		{"success receive with coin from another chain as source", func() {}, false, true},
		{"empty coin", func() {
			trace = types.DenomTrace{}
			amount = sdk.ZeroInt()
		}, true, false},
		{"invalid receiver address", func() {
			receiver = "gaia1scqhwpgsmr6vmztaa7suurfl52my6nd2kmrudl"
		}, true, false},

		// onRecvPacket
		// - coin from chain chainA
		{"failure: mint zero coin", func() {
			amount = sdk.ZeroInt()
		}, false, false},

		// - coin being sent back to original chain (chainB)
		{"tries to unescrow more tokens than allowed", func() {
			amount = sdk.NewInt(1000000)
		}, true, false},

		// - coin being sent to module address on chainA
		{"failure: receive on module account", func() {
			receiver = suite.chainA.GetSimApp().AccountKeeper.GetModuleAddress(types.ModuleName).String()
		}, false, false},

		// - coin being sent back to original chain (chainB) to module address
		{"failure: receive on module account on source chain", func() {
			receiver = suite.chainB.GetSimApp().AccountKeeper.GetModuleAddress(types.ModuleName).String()
		}, true, false},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(fmt.Sprintf("Case %s", tc.msg), func() {
			suite.SetupTest() // reset

			path := NewTransferPath(suite.chainA, suite.chainB)
			suite.coordinator.Setup(path)
			receiver = suite.chainB.SenderAccount.GetAddress().String() // must be explicitly changed in malleate

			amount = sdk.NewInt(100) // must be explicitly changed in malleate
			seq := uint64(1)

			if tc.recvIsSource {
				// send coin from chainB to chainA, receive them, acknowledge them, and send back to chainB
				coinFromBToA := sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(100))
				transferMsg := types.NewMsgTransfer(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, coinFromBToA, suite.chainB.SenderAccount.GetAddress().String(), suite.chainA.SenderAccount.GetAddress().String(), clienttypes.NewHeight(0, 110), 0)
				res, err := suite.chainB.SendMsgs(transferMsg)
				suite.Require().NoError(err) // message committed

				packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents())
				suite.Require().NoError(err)

				err = path.RelayPacket(packet)
				suite.Require().NoError(err) // relay committed

				seq++

				// NOTE: trace must be explicitly changed in malleate to test invalid cases
				trace = types.ParseDenomTrace(types.GetPrefixedDenom(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, sdk.DefaultBondDenom))
			} else {
				trace = types.ParseDenomTrace(sdk.DefaultBondDenom)
			}

			// send coin from chainA to chainB
			transferMsg := types.NewMsgTransfer(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, sdk.NewCoin(trace.IBCDenom(), amount), suite.chainA.SenderAccount.GetAddress().String(), receiver, clienttypes.NewHeight(0, 110), 0)
			_, err := suite.chainA.SendMsgs(transferMsg)
			suite.Require().NoError(err) // message committed

			tc.malleate()

			data := types.NewFungibleTokenPacketData(trace.GetFullDenomPath(), amount.String(), suite.chainA.SenderAccount.GetAddress().String(), receiver)
			packet := channeltypes.NewPacket(data.GetBytes(), seq, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.NewHeight(0, 100), 0)

			err = suite.chainB.GetSimApp().TransferKeeper.OnRecvPacket(suite.chainB.GetContext(), packet, data)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

// TestOnAcknowledgementPacket tests that successful acknowledgement is a no-op
// and failure acknowledment leads to refund when attempting to send from chainA
// to chainB. If sender is source than the denomination being refunded has no
// trace.
func (suite *KeeperTestSuite) TestOnAcknowledgementPacket() {
	var (
		successAck = channeltypes.NewResultAcknowledgement([]byte{byte(1)})
		failedAck  = channeltypes.NewErrorAcknowledgement("failed packet transfer")
		trace      types.DenomTrace
		amount     sdk.Int
		path       *ibctesting.Path
	)

	testCases := []struct {
		msg      string
		ack      channeltypes.Acknowledgement
		malleate func()
		success  bool // success of ack
		expPass  bool
	}{
		{"success ack causes no-op", successAck, func() {
			trace = types.ParseDenomTrace(types.GetPrefixedDenom(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, sdk.DefaultBondDenom))
		}, true, true},
		{"successful refund from source chain", failedAck, func() {
			escrow := types.GetEscrowAddress(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			trace = types.ParseDenomTrace(sdk.DefaultBondDenom)
			coin := sdk.NewCoin(sdk.DefaultBondDenom, amount)

			suite.Require().NoError(simapp.FundAccount(suite.chainA.GetSimApp(), suite.chainA.GetContext(), escrow, sdk.NewCoins(coin)))
		}, false, true},
		{"unsuccessful refund from source", failedAck,
			func() {
				trace = types.ParseDenomTrace(sdk.DefaultBondDenom)
			}, false, false},
		{"successful refund from with coin from external chain", failedAck,
			func() {
				escrow := types.GetEscrowAddress(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
				trace = types.ParseDenomTrace(types.GetPrefixedDenom(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, sdk.DefaultBondDenom))
				coin := sdk.NewCoin(trace.IBCDenom(), amount)

				suite.Require().NoError(simapp.FundAccount(suite.chainA.GetSimApp(), suite.chainA.GetContext(), escrow, sdk.NewCoins(coin)))
			}, false, true},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(fmt.Sprintf("Case %s", tc.msg), func() {
			suite.SetupTest() // reset
			path = NewTransferPath(suite.chainA, suite.chainB)
			suite.coordinator.Setup(path)
			amount = sdk.NewInt(100) // must be explicitly changed

			tc.malleate()

			data := types.NewFungibleTokenPacketData(trace.GetFullDenomPath(), amount.String(), suite.chainA.SenderAccount.GetAddress().String(), suite.chainB.SenderAccount.GetAddress().String())
			packet := channeltypes.NewPacket(data.GetBytes(), 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.NewHeight(0, 100), 0)

			preCoin := suite.chainA.GetSimApp().BankKeeper.GetBalance(suite.chainA.GetContext(), suite.chainA.SenderAccount.GetAddress(), trace.IBCDenom())

			err := suite.chainA.GetSimApp().TransferKeeper.OnAcknowledgementPacket(suite.chainA.GetContext(), packet, data, tc.ack)
			if tc.expPass {
				suite.Require().NoError(err)
				postCoin := suite.chainA.GetSimApp().BankKeeper.GetBalance(suite.chainA.GetContext(), suite.chainA.SenderAccount.GetAddress(), trace.IBCDenom())
				deltaAmount := postCoin.Amount.Sub(preCoin.Amount)

				if tc.success {
					suite.Require().Equal(int64(0), deltaAmount.Int64(), "successful ack changed balance")
				} else {
					suite.Require().Equal(amount, deltaAmount, "failed ack did not trigger refund")
				}

			} else {
				suite.Require().Error(err)
			}
		})
	}
}

// TestOnTimeoutPacket test private refundPacket function since it is a simple
// wrapper over it. The actual timeout does not matter since IBC core logic
// is not being tested. The test is timing out a send from chainA to chainB
// so the refunds are occurring on chainA.
func (suite *KeeperTestSuite) TestOnTimeoutPacket() {
	var (
		trace  types.DenomTrace
		path   *ibctesting.Path
		amount sdk.Int
		sender string
	)

	testCases := []struct {
		msg      string
		malleate func()
		expPass  bool
	}{
		{"successful timeout from sender as source chain",
			func() {
				escrow := types.GetEscrowAddress(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
				trace = types.ParseDenomTrace(sdk.DefaultBondDenom)
				coin := sdk.NewCoin(trace.IBCDenom(), amount)

				suite.Require().NoError(simapp.FundAccount(suite.chainA.GetSimApp(), suite.chainA.GetContext(), escrow, sdk.NewCoins(coin)))
			}, true},
		{"successful timeout from external chain",
			func() {
				escrow := types.GetEscrowAddress(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
				trace = types.ParseDenomTrace(types.GetPrefixedDenom(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, sdk.DefaultBondDenom))
				coin := sdk.NewCoin(trace.IBCDenom(), amount)

				suite.Require().NoError(simapp.FundAccount(suite.chainA.GetSimApp(), suite.chainA.GetContext(), escrow, sdk.NewCoins(coin)))
			}, true},
		{"no balance for coin denom",
			func() {
				trace = types.ParseDenomTrace("bitcoin")
			}, false},
		{"unescrow failed",
			func() {
				trace = types.ParseDenomTrace(sdk.DefaultBondDenom)
			}, false},
		{"mint failed",
			func() {
				trace = types.ParseDenomTrace(types.GetPrefixedDenom(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, sdk.DefaultBondDenom))
				amount = sdk.OneInt()
				sender = "invalid address"
			}, false},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(fmt.Sprintf("Case %s", tc.msg), func() {
			suite.SetupTest() // reset

			path = NewTransferPath(suite.chainA, suite.chainB)
			suite.coordinator.Setup(path)
			amount = sdk.NewInt(100) // must be explicitly changed
			sender = suite.chainA.SenderAccount.GetAddress().String()

			tc.malleate()

			data := types.NewFungibleTokenPacketData(trace.GetFullDenomPath(), amount.String(), sender, suite.chainB.SenderAccount.GetAddress().String())
			packet := channeltypes.NewPacket(data.GetBytes(), 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.NewHeight(0, 100), 0)

			preCoin := suite.chainA.GetSimApp().BankKeeper.GetBalance(suite.chainA.GetContext(), suite.chainA.SenderAccount.GetAddress(), trace.IBCDenom())

			err := suite.chainA.GetSimApp().TransferKeeper.OnTimeoutPacket(suite.chainA.GetContext(), packet, data)

			postCoin := suite.chainA.GetSimApp().BankKeeper.GetBalance(suite.chainA.GetContext(), suite.chainA.SenderAccount.GetAddress(), trace.IBCDenom())
			deltaAmount := postCoin.Amount.Sub(preCoin.Amount)

			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().Equal(amount.Int64(), deltaAmount.Int64(), "successful timeout did not trigger refund")
			} else {
				suite.Require().Error(err)
			}
		})
	}
}
