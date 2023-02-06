package wasm_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	"github.com/stretchr/testify/require"

	wasmibctesting "github.com/CosmWasm/wasmd/x/wasm/ibctesting"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
)

func TestIBCReflectContract(t *testing.T) {
	// scenario:
	//  chain A: ibc_reflect_send.wasm
	//  chain B: reflect.wasm + ibc_reflect.wasm
	//
	//  Chain A "ibc_reflect_send" sends a IBC packet "on channel connect" event to chain B "ibc_reflect"
	//  "ibc_reflect" sends a submessage to "reflect" which is returned as submessage.

	var (
		coordinator = wasmibctesting.NewCoordinator(t, 2)
		chainA      = coordinator.GetChain(wasmibctesting.GetChainID(0))
		chainB      = coordinator.GetChain(wasmibctesting.GetChainID(1))
	)
	coordinator.CommitBlock(chainA, chainB)

	initMsg := []byte(`{}`)
	codeID := chainA.StoreCodeFile("./keeper/testdata/ibc_reflect_send.wasm").CodeID
	sendContractAddr := chainA.InstantiateContract(codeID, initMsg)

	reflectID := chainB.StoreCodeFile("./keeper/testdata/reflect.wasm").CodeID
	initMsg = wasmkeeper.IBCReflectInitMsg{
		ReflectCodeID: reflectID,
	}.GetBytes(t)
	codeID = chainB.StoreCodeFile("./keeper/testdata/ibc_reflect.wasm").CodeID

	reflectContractAddr := chainB.InstantiateContract(codeID, initMsg)
	var (
		sourcePortID      = chainA.ContractInfo(sendContractAddr).IBCPortID
		counterpartPortID = chainB.ContractInfo(reflectContractAddr).IBCPortID
	)
	coordinator.CommitBlock(chainA, chainB)
	coordinator.UpdateTime()

	require.Equal(t, chainA.CurrentHeader.Time, chainB.CurrentHeader.Time)
	path := wasmibctesting.NewPath(chainA, chainB)
	path.EndpointA.ChannelConfig = &ibctesting.ChannelConfig{
		PortID:  sourcePortID,
		Version: "ibc-reflect-v1",
		Order:   channeltypes.ORDERED,
	}
	path.EndpointB.ChannelConfig = &ibctesting.ChannelConfig{
		PortID:  counterpartPortID,
		Version: "ibc-reflect-v1",
		Order:   channeltypes.ORDERED,
	}

	coordinator.SetupConnections(path)
	coordinator.CreateChannels(path)

	// TODO: query both contracts directly to ensure they have registered the proper connection
	// (and the chainB has created a reflect contract)

	// there should be one packet to relay back and forth (whoami)
	// TODO: how do I find the packet that was previously sent by the smart contract?
	// Coordinator.RecvPacket requires channeltypes.Packet as input?
	// Given the source (portID, channelID), we should be able to count how many packets are pending, query the data
	// and submit them to the other side (same with acks). This is what the real relayer does. I guess the test framework doesn't?

	// Update: I dug through the code, especially channel.Keeper.SendPacket, and it only writes a commitment
	// only writes I see: https://github.com/cosmos/cosmos-sdk/blob/31fdee0228bd6f3e787489c8e4434aabc8facb7d/x/ibc/core/04-channel/keeper/packet.go#L115-L116
	// commitment is hashed packet: https://github.com/cosmos/cosmos-sdk/blob/31fdee0228bd6f3e787489c8e4434aabc8facb7d/x/ibc/core/04-channel/types/packet.go#L14-L34
	// how is the relayer supposed to get the original packet data??
	// eg. ibctransfer doesn't store the packet either: https://github.com/cosmos/cosmos-sdk/blob/master/x/ibc/applications/transfer/keeper/relay.go#L145-L162
	// ... or I guess the original packet data is only available in the event logs????
	// https://github.com/cosmos/cosmos-sdk/blob/31fdee0228bd6f3e787489c8e4434aabc8facb7d/x/ibc/core/04-channel/keeper/packet.go#L121-L132

	// ensure the expected packet was prepared, and relay it
	require.Equal(t, 1, len(chainA.PendingSendPackets))
	require.Equal(t, 0, len(chainB.PendingSendPackets))
	err := coordinator.RelayAndAckPendingPackets(path)
	require.NoError(t, err)
	require.Equal(t, 0, len(chainA.PendingSendPackets))
	require.Equal(t, 0, len(chainB.PendingSendPackets))

	// let's query the source contract and make sure it registered an address
	query := ReflectSendQueryMsg{Account: &AccountQuery{ChannelID: path.EndpointA.ChannelID}}
	var account AccountResponse
	err = chainA.SmartQuery(sendContractAddr.String(), query, &account)
	require.NoError(t, err)
	require.NotEmpty(t, account.RemoteAddr)
	require.Empty(t, account.RemoteBalance)

	// close channel
	coordinator.CloseChannel(path)

	// let's query the source contract and make sure it registered an address
	account = AccountResponse{}
	err = chainA.SmartQuery(sendContractAddr.String(), query, &account)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

type ReflectSendQueryMsg struct {
	Admin        *struct{}     `json:"admin,omitempty"`
	ListAccounts *struct{}     `json:"list_accounts,omitempty"`
	Account      *AccountQuery `json:"account,omitempty"`
}

type AccountQuery struct {
	ChannelID string `json:"channel_id"`
}

type AccountResponse struct {
	LastUpdateTime uint64            `json:"last_update_time,string"`
	RemoteAddr     string            `json:"remote_addr"`
	RemoteBalance  wasmvmtypes.Coins `json:"remote_balance"`
}
