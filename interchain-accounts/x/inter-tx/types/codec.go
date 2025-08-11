package types

import (
	"github.com/sei-protocol/sei-chain/cosmos-sdk/codec"
	cdctypes "github.com/sei-protocol/sei-chain/cosmos-sdk/codec/types"

	// this line is used by starport scaffolding # 1
	sdk "github.com/sei-protocol/sei-chain/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/types/msgservice"
)

var (
	amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewAminoCodec(amino)
)

func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(MsgRegisterAccount{}, "intertx/MsgRegisterAccount", nil)
	cdc.RegisterConcrete(MsgSubmitTx{}, "intertx/MsgSubmitTx", nil)
}

func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgRegisterAccount{},
		&MsgSubmitTx{},
	)
}
