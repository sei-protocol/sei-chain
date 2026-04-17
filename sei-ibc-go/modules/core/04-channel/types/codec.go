package types

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/msgservice"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/exported"
)

// RegisterInterfaces register the ibc channel submodule interfaces to protobuf
// Any.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterInterface(
		"ibc.core.channel.v1.ChannelI",
		(*exported.ChannelI)(nil),
		&Channel{},
	)
	registry.RegisterInterface(
		"ibc.core.channel.v1.CounterpartyChannelI",
		(*exported.CounterpartyChannelI)(nil),
		&Counterparty{},
	)
	registry.RegisterInterface(
		"ibc.core.channel.v1.PacketI",
		(*exported.PacketI)(nil),
		&Packet{},
	)
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgChannelOpenInit{},
		&MsgChannelOpenTry{},
		&MsgChannelOpenAck{},
		&MsgChannelOpenConfirm{},
		&MsgChannelCloseInit{},
		&MsgChannelCloseConfirm{},
		&MsgRecvPacket{},
		&MsgAcknowledgement{},
		&MsgTimeout{},
		&MsgTimeoutOnClose{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

// SubModuleCdc references the global x/ibc/core/04-channel module codec. Note, the codec should
// ONLY be used in certain instances of tests and for JSON encoding.
//
// The actual codec used for serialization should be provided to x/ibc/core/04-channel and
// defined at the application level.
var SubModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
