package types

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	cdctypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	// this line is used by starport scaffolding # 1
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/msgservice"
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
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateDenom{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateDenom{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgMint{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgBurn{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgChangeAdmin{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
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
