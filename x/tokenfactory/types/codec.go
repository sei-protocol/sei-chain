package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	// this line is used by starport scaffolding # 1
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgCreateDenom{}, "tokenfactory/MsgCreateDenom", nil)
	cdc.RegisterConcrete(&MsgUpdateDenom{}, "tokenfactory/MsgUpdateDenom", nil)
	cdc.RegisterConcrete(&MsgMint{}, "tokenfactory/MsgMint", nil)
	cdc.RegisterConcrete(&MsgBurn{}, "tokenfactory/MsgBurn", nil)
	cdc.RegisterConcrete(&MsgChangeAdmin{}, "tokenfactory/MsgChangeAdmin", nil)
	cdc.RegisterConcrete(&MsgSetDenomMetadata{}, "tokenfactory/MsgSetDenomMetadata", nil)
}

func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*seitypes.Msg)(nil),
		&MsgCreateDenom{},
	)
	registry.RegisterImplementations((*seitypes.Msg)(nil),
		&MsgUpdateDenom{},
	)
	registry.RegisterImplementations((*seitypes.Msg)(nil),
		&MsgMint{},
	)
	registry.RegisterImplementations((*seitypes.Msg)(nil),
		&MsgBurn{},
	)
	registry.RegisterImplementations((*seitypes.Msg)(nil),
		&MsgChangeAdmin{},
	)
	registry.RegisterImplementations((*seitypes.Msg)(nil),
		&MsgSetDenomMetadata{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
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
