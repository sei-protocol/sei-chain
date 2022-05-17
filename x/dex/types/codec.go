package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgPlaceOrders{}, "dex/MsgPlaceOrders", nil)
	cdc.RegisterConcrete(&MsgCancelOrders{}, "dex/MsgCancelOrders", nil)
	cdc.RegisterConcrete(&MsgLiquidation{}, "dex/MsgLiquidation", nil)
	// this line is used by starport scaffolding # 2
}

func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgPlaceOrders{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCancelOrders{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgLiquidation{},
	)
	// this line is used by starport scaffolding # 3

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

var (
	Amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewProtoCodec(cdctypes.NewInterfaceRegistry())
)
