package wasm_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	wasmvm "github.com/CosmWasm/wasmvm"
	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	ibctransfertypes "github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	wasmibctesting "github.com/CosmWasm/wasmd/x/wasm/ibctesting"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtesting "github.com/CosmWasm/wasmd/x/wasm/keeper/wasmtesting"
	"github.com/CosmWasm/wasmd/x/wasm/types"
)

func TestFromIBCTransferToContract(t *testing.T) {
	// scenario: given two chains,
	//           with a contract on chain B
	//           then the contract can handle the receiving side of an ics20 transfer
	//           that was started on chain A via ibc transfer module

	transferAmount := sdk.NewInt(1)
	specs := map[string]struct {
		contract             wasmtesting.IBCContractCallbacks
		setupContract        func(t *testing.T, contract wasmtesting.IBCContractCallbacks, chain *wasmibctesting.TestChain)
		expChainABalanceDiff sdk.Int
		expChainBBalanceDiff sdk.Int
	}{
		"ack": {
			contract: &ackReceiverContract{},
			setupContract: func(t *testing.T, contract wasmtesting.IBCContractCallbacks, chain *wasmibctesting.TestChain) {
				c := contract.(*ackReceiverContract)
				c.t = t
				c.chain = chain
			},
			expChainABalanceDiff: transferAmount.Neg(),
			expChainBBalanceDiff: transferAmount,
		},
		"nack": {
			contract: &nackReceiverContract{},
			setupContract: func(t *testing.T, contract wasmtesting.IBCContractCallbacks, chain *wasmibctesting.TestChain) {
				c := contract.(*nackReceiverContract)
				c.t = t
			},
			expChainABalanceDiff: sdk.ZeroInt(),
			expChainBBalanceDiff: sdk.ZeroInt(),
		},
		"error": {
			contract: &errorReceiverContract{},
			setupContract: func(t *testing.T, contract wasmtesting.IBCContractCallbacks, chain *wasmibctesting.TestChain) {
				c := contract.(*errorReceiverContract)
				c.t = t
			},
			expChainABalanceDiff: sdk.ZeroInt(),
			expChainBBalanceDiff: sdk.ZeroInt(),
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			var (
				chainAOpts = []wasmkeeper.Option{wasmkeeper.WithWasmEngine(
					wasmtesting.NewIBCContractMockWasmer(spec.contract),
				)}
				coordinator = wasmibctesting.NewCoordinator(t, 2, []wasmkeeper.Option{}, chainAOpts)
				chainA      = coordinator.GetChain(wasmibctesting.GetChainID(0))
				chainB      = coordinator.GetChain(wasmibctesting.GetChainID(1))
			)
			coordinator.CommitBlock(chainA, chainB)
			myContractAddr := chainB.SeedNewContractInstance()
			contractBPortID := chainB.ContractInfo(myContractAddr).IBCPortID

			spec.setupContract(t, spec.contract, chainB)

			path := wasmibctesting.NewPath(chainA, chainB)
			path.EndpointA.ChannelConfig = &wasmibctesting.ChannelConfig{
				PortID:  "transfer",
				Version: ibctransfertypes.Version,
				Order:   channeltypes.UNORDERED,
			}
			path.EndpointB.ChannelConfig = &wasmibctesting.ChannelConfig{
				PortID:  contractBPortID,
				Version: ibctransfertypes.Version,
				Order:   channeltypes.UNORDERED,
			}

			coordinator.SetupConnections(path)
			coordinator.CreateChannels(path)

			originalChainABalance := chainA.Balance(chainA.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
			// when transfer via sdk transfer from A (module) -> B (contract)
			coinToSendToB := sdk.NewCoin(sdk.DefaultBondDenom, transferAmount)
			timeoutHeight := clienttypes.NewHeight(1, 110)
			msg := ibctransfertypes.NewMsgTransfer(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, coinToSendToB, chainA.SenderAccount.GetAddress().String(), chainB.SenderAccount.GetAddress().String(), timeoutHeight, 0)
			_, err := chainA.SendMsgs(msg)
			require.NoError(t, err)
			require.NoError(t, path.EndpointB.UpdateClient())

			// then
			require.Equal(t, 1, len(chainA.PendingSendPackets))
			require.Equal(t, 0, len(chainB.PendingSendPackets))

			// and when relay to chain B and handle Ack on chain A
			err = coordinator.RelayAndAckPendingPackets(path)
			require.NoError(t, err)

			// then
			require.Equal(t, 0, len(chainA.PendingSendPackets))
			require.Equal(t, 0, len(chainB.PendingSendPackets))

			// and source chain balance was decreased
			newChainABalance := chainA.Balance(chainA.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
			assert.Equal(t, originalChainABalance.Amount.Add(spec.expChainABalanceDiff), newChainABalance.Amount)

			// and dest chain balance contains voucher
			expBalance := ibctransfertypes.GetTransferCoin(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, coinToSendToB.Denom, spec.expChainBBalanceDiff)
			gotBalance := chainB.Balance(chainB.SenderAccount.GetAddress(), expBalance.Denom)
			assert.Equal(t, expBalance, gotBalance, "got total balance: %s", chainB.AllBalances(chainB.SenderAccount.GetAddress()))
		})
	}
}

func TestContractCanInitiateIBCTransferMsg(t *testing.T) {
	// scenario: given two chains,
	//           with a contract on chain A
	//           then the contract can start an ibc transfer via ibctransfertypes.NewMsgTransfer
	//           that is handled on chain A by the ibc transfer module and
	//           received on chain B via ibc transfer module as well

	myContract := &sendViaIBCTransferContract{t: t}
	var (
		chainAOpts = []wasmkeeper.Option{
			wasmkeeper.WithWasmEngine(
				wasmtesting.NewIBCContractMockWasmer(myContract)),
		}
		coordinator = wasmibctesting.NewCoordinator(t, 2, chainAOpts)
		chainA      = coordinator.GetChain(wasmibctesting.GetChainID(0))
		chainB      = coordinator.GetChain(wasmibctesting.GetChainID(1))
	)
	myContractAddr := chainA.SeedNewContractInstance()
	coordinator.CommitBlock(chainA, chainB)

	path := wasmibctesting.NewPath(chainA, chainB)
	path.EndpointA.ChannelConfig = &wasmibctesting.ChannelConfig{
		PortID:  ibctransfertypes.PortID,
		Version: ibctransfertypes.Version,
		Order:   channeltypes.UNORDERED,
	}
	path.EndpointB.ChannelConfig = &wasmibctesting.ChannelConfig{
		PortID:  ibctransfertypes.PortID,
		Version: ibctransfertypes.Version,
		Order:   channeltypes.UNORDERED,
	}
	coordinator.SetupConnections(path)
	coordinator.CreateChannels(path)

	// when contract is triggered to send IBCTransferMsg
	receiverAddress := chainB.SenderAccount.GetAddress()
	coinToSendToB := sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(100))

	// start transfer from chainA to chainB
	startMsg := &types.MsgExecuteContract{
		Sender:   chainA.SenderAccount.GetAddress().String(),
		Contract: myContractAddr.String(),
		Msg: startTransfer{
			ChannelID:    path.EndpointA.ChannelID,
			CoinsToSend:  coinToSendToB,
			ReceiverAddr: receiverAddress.String(),
		}.GetBytes(),
	}
	_, err := chainA.SendMsgs(startMsg)
	require.NoError(t, err)

	// then
	require.Equal(t, 1, len(chainA.PendingSendPackets))
	require.Equal(t, 0, len(chainB.PendingSendPackets))

	// and when relay to chain B and handle Ack on chain A
	err = coordinator.RelayAndAckPendingPackets(path)
	require.NoError(t, err)

	// then
	require.Equal(t, 0, len(chainA.PendingSendPackets))
	require.Equal(t, 0, len(chainB.PendingSendPackets))

	// and dest chain balance contains voucher
	bankKeeperB := chainB.GetTestSupport().BankKeeper()
	expBalance := ibctransfertypes.GetTransferCoin(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, coinToSendToB.Denom, coinToSendToB.Amount)
	gotBalance := chainB.Balance(chainB.SenderAccount.GetAddress(), expBalance.Denom)
	assert.Equal(t, expBalance, gotBalance, "got total balance: %s", bankKeeperB.GetAllBalances(chainB.GetContext(), chainB.SenderAccount.GetAddress()))
}

func TestContractCanEmulateIBCTransferMessage(t *testing.T) {
	// scenario: given two chains,
	//           with a contract on chain A
	//           then the contract can emulate the ibc transfer module in the contract to send an ibc packet
	//           which is received on chain B via ibc transfer module

	myContract := &sendEmulatedIBCTransferContract{t: t}

	var (
		chainAOpts = []wasmkeeper.Option{
			wasmkeeper.WithWasmEngine(
				wasmtesting.NewIBCContractMockWasmer(myContract)),
		}
		coordinator = wasmibctesting.NewCoordinator(t, 2, chainAOpts)

		chainA = coordinator.GetChain(wasmibctesting.GetChainID(0))
		chainB = coordinator.GetChain(wasmibctesting.GetChainID(1))
	)
	myContractAddr := chainA.SeedNewContractInstance()
	myContract.contractAddr = myContractAddr.String()

	path := wasmibctesting.NewPath(chainA, chainB)
	path.EndpointA.ChannelConfig = &wasmibctesting.ChannelConfig{
		PortID:  chainA.ContractInfo(myContractAddr).IBCPortID,
		Version: ibctransfertypes.Version,
		Order:   channeltypes.UNORDERED,
	}
	path.EndpointB.ChannelConfig = &wasmibctesting.ChannelConfig{
		PortID:  ibctransfertypes.PortID,
		Version: ibctransfertypes.Version,
		Order:   channeltypes.UNORDERED,
	}
	coordinator.SetupConnections(path)
	coordinator.CreateChannels(path)

	// when contract is triggered to send the ibc package to chain B
	timeout := uint64(chainB.LastHeader.Header.Time.Add(time.Hour).UnixNano()) // enough time to not timeout
	receiverAddress := chainB.SenderAccount.GetAddress()
	coinToSendToB := sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(100))

	// start transfer from chainA to chainB
	startMsg := &types.MsgExecuteContract{
		Sender:   chainA.SenderAccount.GetAddress().String(),
		Contract: myContractAddr.String(),
		Msg: startTransfer{
			ChannelID:       path.EndpointA.ChannelID,
			CoinsToSend:     coinToSendToB,
			ReceiverAddr:    receiverAddress.String(),
			ContractIBCPort: chainA.ContractInfo(myContractAddr).IBCPortID,
			Timeout:         timeout,
		}.GetBytes(),
		Funds: sdk.NewCoins(coinToSendToB),
	}
	_, err := chainA.SendMsgs(startMsg)
	require.NoError(t, err)

	// then
	require.Equal(t, 1, len(chainA.PendingSendPackets))
	require.Equal(t, 0, len(chainB.PendingSendPackets))

	// and when relay to chain B and handle Ack on chain A
	err = coordinator.RelayAndAckPendingPackets(path)
	require.NoError(t, err)

	// then
	require.Equal(t, 0, len(chainA.PendingSendPackets))
	require.Equal(t, 0, len(chainB.PendingSendPackets))

	// and dest chain balance contains voucher
	bankKeeperB := chainB.GetTestSupport().BankKeeper()
	expBalance := ibctransfertypes.GetTransferCoin(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, coinToSendToB.Denom, coinToSendToB.Amount)
	gotBalance := chainB.Balance(chainB.SenderAccount.GetAddress(), expBalance.Denom)
	assert.Equal(t, expBalance, gotBalance, "got total balance: %s", bankKeeperB.GetAllBalances(chainB.GetContext(), chainB.SenderAccount.GetAddress()))
}

func TestContractCanEmulateIBCTransferMessageWithTimeout(t *testing.T) {
	// scenario: given two chains,
	//           with a contract on chain A
	//           then the contract can emulate the ibc transfer module in the contract to send an ibc packet
	//           which is not received on chain B and times out

	myContract := &sendEmulatedIBCTransferContract{t: t}

	var (
		chainAOpts = []wasmkeeper.Option{
			wasmkeeper.WithWasmEngine(
				wasmtesting.NewIBCContractMockWasmer(myContract)),
		}
		coordinator = wasmibctesting.NewCoordinator(t, 2, chainAOpts)

		chainA = coordinator.GetChain(wasmibctesting.GetChainID(0))
		chainB = coordinator.GetChain(wasmibctesting.GetChainID(1))
	)
	coordinator.CommitBlock(chainA, chainB)
	myContractAddr := chainA.SeedNewContractInstance()
	myContract.contractAddr = myContractAddr.String()

	path := wasmibctesting.NewPath(chainA, chainB)
	path.EndpointA.ChannelConfig = &wasmibctesting.ChannelConfig{
		PortID:  chainA.ContractInfo(myContractAddr).IBCPortID,
		Version: ibctransfertypes.Version,
		Order:   channeltypes.UNORDERED,
	}
	path.EndpointB.ChannelConfig = &wasmibctesting.ChannelConfig{
		PortID:  ibctransfertypes.PortID,
		Version: ibctransfertypes.Version,
		Order:   channeltypes.UNORDERED,
	}
	coordinator.SetupConnections(path)
	coordinator.CreateChannels(path)
	coordinator.UpdateTime()

	// when contract is triggered to send the ibc package to chain B
	timeout := uint64(chainB.LastHeader.Header.Time.Add(time.Nanosecond).UnixNano()) // will timeout
	receiverAddress := chainB.SenderAccount.GetAddress()
	coinToSendToB := sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(100))
	initialContractBalance := chainA.Balance(myContractAddr, sdk.DefaultBondDenom)
	initialSenderBalance := chainA.Balance(chainA.SenderAccount.GetAddress(), sdk.DefaultBondDenom)

	// custom payload data to be transferred into a proper ICS20 ibc packet
	startMsg := &types.MsgExecuteContract{
		Sender:   chainA.SenderAccount.GetAddress().String(),
		Contract: myContractAddr.String(),
		Msg: startTransfer{
			ChannelID:       path.EndpointA.ChannelID,
			CoinsToSend:     coinToSendToB,
			ReceiverAddr:    receiverAddress.String(),
			ContractIBCPort: chainA.ContractInfo(myContractAddr).IBCPortID,
			Timeout:         timeout,
		}.GetBytes(),
		Funds: sdk.NewCoins(coinToSendToB),
	}
	_, err := chainA.SendMsgs(startMsg)
	require.NoError(t, err)
	coordinator.CommitBlock(chainA, chainB)
	// then
	require.Equal(t, 1, len(chainA.PendingSendPackets))
	require.Equal(t, 0, len(chainB.PendingSendPackets))
	newContractBalance := chainA.Balance(myContractAddr, sdk.DefaultBondDenom)
	assert.Equal(t, initialContractBalance.Add(coinToSendToB), newContractBalance) // hold in escrow

	// when timeout packet send (by the relayer)
	err = coordinator.TimeoutPendingPackets(path)
	require.NoError(t, err)
	coordinator.CommitBlock(chainA)

	// then
	require.Equal(t, 0, len(chainA.PendingSendPackets))
	require.Equal(t, 0, len(chainB.PendingSendPackets))

	// and then verify account balances restored
	newContractBalance = chainA.Balance(myContractAddr, sdk.DefaultBondDenom)
	assert.Equal(t, initialContractBalance.String(), newContractBalance.String())
	newSenderBalance := chainA.Balance(chainA.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
	assert.Equal(t, initialSenderBalance.String(), newSenderBalance.String())
}

func TestContractHandlesChannelClose(t *testing.T) {
	// scenario: a contract is the sending side of an ics20 transfer but the packet was not received
	// on the destination chain within the timeout boundaries
	myContractA := &captureCloseContract{}
	myContractB := &captureCloseContract{}

	var (
		chainAOpts = []wasmkeeper.Option{
			wasmkeeper.WithWasmEngine(
				wasmtesting.NewIBCContractMockWasmer(myContractA)),
		}
		chainBOpts = []wasmkeeper.Option{
			wasmkeeper.WithWasmEngine(
				wasmtesting.NewIBCContractMockWasmer(myContractB)),
		}
		coordinator = wasmibctesting.NewCoordinator(t, 2, chainAOpts, chainBOpts)

		chainA = coordinator.GetChain(wasmibctesting.GetChainID(0))
		chainB = coordinator.GetChain(wasmibctesting.GetChainID(1))
	)

	coordinator.CommitBlock(chainA, chainB)
	myContractAddrA := chainA.SeedNewContractInstance()
	_ = chainB.SeedNewContractInstance() // skip one instance
	myContractAddrB := chainB.SeedNewContractInstance()

	path := wasmibctesting.NewPath(chainA, chainB)
	path.EndpointA.ChannelConfig = &wasmibctesting.ChannelConfig{
		PortID:  chainA.ContractInfo(myContractAddrA).IBCPortID,
		Version: ibctransfertypes.Version,
		Order:   channeltypes.UNORDERED,
	}
	path.EndpointB.ChannelConfig = &wasmibctesting.ChannelConfig{
		PortID:  chainB.ContractInfo(myContractAddrB).IBCPortID,
		Version: ibctransfertypes.Version,
		Order:   channeltypes.UNORDERED,
	}
	coordinator.SetupConnections(path)
	coordinator.CreateChannels(path)
	coordinator.CloseChannel(path)
	assert.True(t, myContractB.closeCalled)
}

var _ wasmtesting.IBCContractCallbacks = &captureCloseContract{}

// contract that sets a flag on IBC channel close only.
type captureCloseContract struct {
	contractStub
	closeCalled bool
}

func (c *captureCloseContract) IBCChannelClose(codeID wasmvm.Checksum, env wasmvmtypes.Env, msg wasmvmtypes.IBCChannelCloseMsg, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.IBCBasicResponse, uint64, error) {
	c.closeCalled = true
	return &wasmvmtypes.IBCBasicResponse{}, 1, nil
}

var _ wasmtesting.IBCContractCallbacks = &sendViaIBCTransferContract{}

// contract that initiates an ics-20 transfer on execute via sdk message
type sendViaIBCTransferContract struct {
	contractStub
	t *testing.T
}

func (s *sendViaIBCTransferContract) Execute(code wasmvm.Checksum, env wasmvmtypes.Env, info wasmvmtypes.MessageInfo, executeMsg []byte, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
	var in startTransfer
	if err := json.Unmarshal(executeMsg, &in); err != nil {
		return nil, 0, err
	}
	ibcMsg := &wasmvmtypes.IBCMsg{
		Transfer: &wasmvmtypes.TransferMsg{
			ToAddress: in.ReceiverAddr,
			Amount:    wasmvmtypes.NewCoin(in.CoinsToSend.Amount.Uint64(), in.CoinsToSend.Denom),
			ChannelID: in.ChannelID,
			Timeout: wasmvmtypes.IBCTimeout{Block: &wasmvmtypes.IBCTimeoutBlock{
				Revision: 0,
				Height:   110,
			}},
		},
	}

	return &wasmvmtypes.Response{Messages: []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{IBC: ibcMsg}}}}, 0, nil
}

var _ wasmtesting.IBCContractCallbacks = &sendEmulatedIBCTransferContract{}

// contract that interacts as an ics20 sending side via IBC packets
// It can also handle the timeout.
type sendEmulatedIBCTransferContract struct {
	contractStub
	t            *testing.T
	contractAddr string
}

func (s *sendEmulatedIBCTransferContract) Execute(code wasmvm.Checksum, env wasmvmtypes.Env, info wasmvmtypes.MessageInfo, executeMsg []byte, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
	var in startTransfer
	if err := json.Unmarshal(executeMsg, &in); err != nil {
		return nil, 0, err
	}
	require.Len(s.t, info.Funds, 1)
	require.Equal(s.t, in.CoinsToSend.Amount.String(), info.Funds[0].Amount)
	require.Equal(s.t, in.CoinsToSend.Denom, info.Funds[0].Denom)
	dataPacket := ibctransfertypes.NewFungibleTokenPacketData(
		in.CoinsToSend.Denom, in.CoinsToSend.Amount.String(), info.Sender, in.ReceiverAddr,
	)
	if err := dataPacket.ValidateBasic(); err != nil {
		return nil, 0, err
	}

	ibcMsg := &wasmvmtypes.IBCMsg{
		SendPacket: &wasmvmtypes.SendPacketMsg{
			ChannelID: in.ChannelID,
			Data:      dataPacket.GetBytes(),
			Timeout:   wasmvmtypes.IBCTimeout{Timestamp: in.Timeout},
		},
	}
	return &wasmvmtypes.Response{Messages: []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{IBC: ibcMsg}}}}, 0, nil
}

func (c *sendEmulatedIBCTransferContract) IBCPacketTimeout(codeID wasmvm.Checksum, env wasmvmtypes.Env, msg wasmvmtypes.IBCPacketTimeoutMsg, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.IBCBasicResponse, uint64, error) {
	packet := msg.Packet

	var data ibctransfertypes.FungibleTokenPacketData
	if err := ibctransfertypes.ModuleCdc.UnmarshalJSON(packet.Data, &data); err != nil {
		return nil, 0, err
	}
	if err := data.ValidateBasic(); err != nil {
		return nil, 0, err
	}
	amount, _ := sdk.NewIntFromString(data.Amount)

	returnTokens := &wasmvmtypes.BankMsg{
		Send: &wasmvmtypes.SendMsg{
			ToAddress: data.Sender,
			Amount:    wasmvmtypes.Coins{wasmvmtypes.NewCoin(amount.Uint64(), data.Denom)},
		},
	}

	return &wasmvmtypes.IBCBasicResponse{Messages: []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Bank: returnTokens}}}}, 0, nil
}

// custom contract execute payload
type startTransfer struct {
	ChannelID       string
	CoinsToSend     sdk.Coin
	ReceiverAddr    string
	ContractIBCPort string
	Timeout         uint64
}

func (g startTransfer) GetBytes() types.RawContractMessage {
	b, err := json.Marshal(g)
	if err != nil {
		panic(err)
	}
	return b
}

var _ wasmtesting.IBCContractCallbacks = &ackReceiverContract{}

// contract that acts as the receiving side for an ics-20 transfer.
type ackReceiverContract struct {
	contractStub
	t     *testing.T
	chain *wasmibctesting.TestChain
}

func (c *ackReceiverContract) IBCPacketReceive(codeID wasmvm.Checksum, env wasmvmtypes.Env, msg wasmvmtypes.IBCPacketReceiveMsg, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.IBCReceiveResult, uint64, error) {
	packet := msg.Packet

	var src ibctransfertypes.FungibleTokenPacketData
	if err := ibctransfertypes.ModuleCdc.UnmarshalJSON(packet.Data, &src); err != nil {
		return nil, 0, err
	}
	require.NoError(c.t, src.ValidateBasic())

	// call original ibctransfer keeper to not copy all code into this
	ibcPacket := toIBCPacket(packet)
	ctx := c.chain.GetContext() // HACK: please note that this is not reverted after checkTX
	err := c.chain.GetTestSupport().TransferKeeper().OnRecvPacket(ctx, ibcPacket, src)
	if err != nil {
		return nil, 0, sdkerrors.Wrap(err, "within our smart contract")
	}

	var log []wasmvmtypes.EventAttribute // note: all events are under `wasm` event type
	ack := channeltypes.NewResultAcknowledgement([]byte{byte(1)}).Acknowledgement()
	return &wasmvmtypes.IBCReceiveResult{Ok: &wasmvmtypes.IBCReceiveResponse{Acknowledgement: ack, Attributes: log}}, 0, nil
}

func (c *ackReceiverContract) IBCPacketAck(codeID wasmvm.Checksum, env wasmvmtypes.Env, msg wasmvmtypes.IBCPacketAckMsg, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.IBCBasicResponse, uint64, error) {
	var data ibctransfertypes.FungibleTokenPacketData
	if err := ibctransfertypes.ModuleCdc.UnmarshalJSON(msg.OriginalPacket.Data, &data); err != nil {
		return nil, 0, err
	}
	// call original ibctransfer keeper to not copy all code into this

	var ack channeltypes.Acknowledgement
	if err := ibctransfertypes.ModuleCdc.UnmarshalJSON(msg.Acknowledgement.Data, &ack); err != nil {
		return nil, 0, err
	}

	// call original ibctransfer keeper to not copy all code into this
	ctx := c.chain.GetContext() // HACK: please note that this is not reverted after checkTX
	ibcPacket := toIBCPacket(msg.OriginalPacket)
	err := c.chain.GetTestSupport().TransferKeeper().OnAcknowledgementPacket(ctx, ibcPacket, data, ack)
	if err != nil {
		return nil, 0, sdkerrors.Wrap(err, "within our smart contract")
	}

	return &wasmvmtypes.IBCBasicResponse{}, 0, nil
}

// contract that acts as the receiving side for an ics-20 transfer and always returns a nack.
type nackReceiverContract struct {
	contractStub
	t *testing.T
}

func (c *nackReceiverContract) IBCPacketReceive(codeID wasmvm.Checksum, env wasmvmtypes.Env, msg wasmvmtypes.IBCPacketReceiveMsg, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.IBCReceiveResult, uint64, error) {
	packet := msg.Packet

	var src ibctransfertypes.FungibleTokenPacketData
	if err := ibctransfertypes.ModuleCdc.UnmarshalJSON(packet.Data, &src); err != nil {
		return nil, 0, err
	}
	require.NoError(c.t, src.ValidateBasic())
	return &wasmvmtypes.IBCReceiveResult{Err: "nack-testing"}, 0, nil
}

// contract that acts as the receiving side for an ics-20 transfer and always returns an error.
type errorReceiverContract struct {
	contractStub
	t *testing.T
}

func (c *errorReceiverContract) IBCPacketReceive(codeID wasmvm.Checksum, env wasmvmtypes.Env, msg wasmvmtypes.IBCPacketReceiveMsg, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.IBCReceiveResult, uint64, error) {
	packet := msg.Packet

	var src ibctransfertypes.FungibleTokenPacketData
	if err := ibctransfertypes.ModuleCdc.UnmarshalJSON(packet.Data, &src); err != nil {
		return nil, 0, err
	}
	require.NoError(c.t, src.ValidateBasic())
	return nil, 0, errors.New("error-testing")
}

// simple helper struct that implements connection setup methods.
type contractStub struct{}

func (s *contractStub) IBCChannelOpen(codeID wasmvm.Checksum, env wasmvmtypes.Env, msg wasmvmtypes.IBCChannelOpenMsg, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.IBC3ChannelOpenResponse, uint64, error) {
	return &wasmvmtypes.IBC3ChannelOpenResponse{}, 0, nil
}

func (s *contractStub) IBCChannelConnect(codeID wasmvm.Checksum, env wasmvmtypes.Env, msg wasmvmtypes.IBCChannelConnectMsg, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.IBCBasicResponse, uint64, error) {
	return &wasmvmtypes.IBCBasicResponse{}, 0, nil
}

func (s *contractStub) IBCChannelClose(codeID wasmvm.Checksum, env wasmvmtypes.Env, msg wasmvmtypes.IBCChannelCloseMsg, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.IBCBasicResponse, uint64, error) {
	panic("implement me")
}

func (s *contractStub) IBCPacketReceive(codeID wasmvm.Checksum, env wasmvmtypes.Env, msg wasmvmtypes.IBCPacketReceiveMsg, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.IBCReceiveResult, uint64, error) {
	panic("implement me")
}

func (s *contractStub) IBCPacketAck(codeID wasmvm.Checksum, env wasmvmtypes.Env, msg wasmvmtypes.IBCPacketAckMsg, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.IBCBasicResponse, uint64, error) {
	return &wasmvmtypes.IBCBasicResponse{}, 0, nil
}

func (s *contractStub) IBCPacketTimeout(codeID wasmvm.Checksum, env wasmvmtypes.Env, msg wasmvmtypes.IBCPacketTimeoutMsg, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.IBCBasicResponse, uint64, error) {
	panic("implement me")
}

func toIBCPacket(p wasmvmtypes.IBCPacket) channeltypes.Packet {
	var height clienttypes.Height
	if p.Timeout.Block != nil {
		height = clienttypes.NewHeight(p.Timeout.Block.Revision, p.Timeout.Block.Height)
	}
	return channeltypes.Packet{
		Sequence:           p.Sequence,
		SourcePort:         p.Src.PortID,
		SourceChannel:      p.Src.ChannelID,
		DestinationPort:    p.Dest.PortID,
		DestinationChannel: p.Dest.ChannelID,
		Data:               p.Data,
		TimeoutHeight:      height,
		TimeoutTimestamp:   p.Timeout.Timestamp,
	}
}
