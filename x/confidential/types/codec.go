package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TODO: Add the tx types we create to this method
func RegisterCodec(cdc *codec.LegacyAmino) {
	//cdc.RegisterConcrete(&MsgCreateDenom{}, "tokenfactory/MsgCreateDenom", nil)
}

// TODO: Add the tx types we create to this method
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	//registry.RegisterImplementations((*sdk.Msg)(nil),
	//	&MsgCreateDenom{},
	//)

	// TODO: _Msg_serviceDesc refers to the grpc.ServiceDesc created in tx.go.pb
	// msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

var (
	amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewAminoCodec(amino)
)

func init() {
	RegisterCodec(amino)
	sdk.RegisterLegacyAminoCodec(amino)

	amino.Seal()
}
