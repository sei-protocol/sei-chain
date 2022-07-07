package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgPlaceOrders{}, "dex/MsgPlaceOrders", nil)
	cdc.RegisterConcrete(&MsgCancelOrders{}, "dex/MsgCancelOrders", nil)
	cdc.RegisterConcrete(&MsgLiquidation{}, "dex/MsgLiquidation", nil)
	cdc.RegisterConcrete(&RegisterPairsProposal{}, "dex/RegisterPairsProposal", nil)
	cdc.RegisterConcrete(&UpdateTickSizeProposal{}, "dex/UpdateTickSizeProposal", nil)
	cdc.RegisterConcrete(&AddAssetMetadataProposal{}, "dex/AddAssetMetadataProposal", nil)
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
	registry.RegisterImplementations((*govtypes.Content)(nil),
		&RegisterPairsProposal{},
	)
	registry.RegisterImplementations((*govtypes.Content)(nil),
		&UpdateTickSizeProposal{},
	)
	registry.RegisterImplementations((*govtypes.Content)(nil),
		&AddAssetMetadataProposal{},
	)
	// this line is used by starport scaffolding # 3

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

var (
	Amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewProtoCodec(cdctypes.NewInterfaceRegistry())
)
