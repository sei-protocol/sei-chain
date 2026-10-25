package wasmbinding

import (
	"testing"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	wasmvmtypes "github.com/sei-protocol/sei-chain/sei-wasmvm/types"
	"github.com/stretchr/testify/require"
)

func TestCustomMessageHandlerRejectsIBCRawPacket(t *testing.T) {
	handler := CustomMessageHandler(nil, nil, nil, nil)
	msg := wasmvmtypes.CosmosMsg{IBC: &wasmvmtypes.IBCMsg{SendPacket: &wasmvmtypes.SendPacketMsg{
		ChannelID: "channel-1",
		Data:      []byte("data"),
	}}}

	events, data, err := handler.DispatchMsg(
		sdk.Context{},
		sdk.AccAddress("contract"),
		"contract-port",
		msg,
		wasmvmtypes.MessageInfo{},
		wasmtypes.CodeInfo{},
	)

	require.ErrorIs(t, err, wasmtypes.ErrUnsupportedForContract)
	require.EqualError(t, err, "ibc send packet: unsupported for this contract")
	require.Nil(t, events)
	require.Nil(t, data)
}
