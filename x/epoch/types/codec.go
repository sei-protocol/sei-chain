package types

import (
	"github.com/sei-protocol/sei-chain/cosmos/codec"
	cdctypes "github.com/sei-protocol/sei-chain/cosmos/codec/types"

	// this line is used by starport scaffolding # 1
	"github.com/sei-protocol/sei-chain/cosmos/types/msgservice"
)

func RegisterCodec(_ *codec.LegacyAmino) {}

func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

var (
	amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewAminoCodec(amino)
)

func init() {
	RegisterCodec(amino)
	amino.Seal()
}
