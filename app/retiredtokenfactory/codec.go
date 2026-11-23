package retiredtokenfactory

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateDenom{},
		&MsgUpdateDenom{},
		&MsgMint{},
		&MsgBurn{},
		&MsgChangeAdmin{},
		&MsgSetDenomMetadata{},
	)
}

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgCreateDenom{}, "tokenfactory/MsgCreateDenom", nil)
	cdc.RegisterConcrete(&MsgUpdateDenom{}, "tokenfactory/MsgUpdateDenom", nil)
	cdc.RegisterConcrete(&MsgMint{}, "tokenfactory/MsgMint", nil)
	cdc.RegisterConcrete(&MsgBurn{}, "tokenfactory/MsgBurn", nil)
	cdc.RegisterConcrete(&MsgChangeAdmin{}, "tokenfactory/MsgChangeAdmin", nil)
	cdc.RegisterConcrete(&MsgSetDenomMetadata{}, "tokenfactory/MsgSetDenomMetadata", nil)
}

var (
	amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewAminoCodec(amino)
)

func init() {
	RegisterLegacyAminoCodec(amino)
	sdk.RegisterLegacyAminoCodec(amino)
	amino.Seal()
}
