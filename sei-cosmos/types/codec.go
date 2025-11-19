package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

const (
	// MsgInterfaceProtoName defines the protobuf name of the cosmos Msg interface
	MsgInterfaceProtoName = "cosmos.base.v1beta1.Msg"
)

// RegisterLegacyAminoCodec registers the sdk message type.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterInterface((*seitypes.Msg)(nil), nil)
	cdc.RegisterInterface((*seitypes.Tx)(nil), nil)
}

// RegisterInterfaces registers the sdk message type.
func RegisterInterfaces(registry types.InterfaceRegistry) {
	registry.RegisterInterface(MsgInterfaceProtoName, (*seitypes.Msg)(nil))
}
