package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	// this line is used by starport scaffolding # 1
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgCreateDenom{}, "tokenfactory/create-denom", nil)
	cdc.RegisterConcrete(&MsgMint{}, "tokenfactory/mint", nil)
	cdc.RegisterConcrete(&MsgBurn{}, "tokenfactory/burn", nil)
	// cdc.RegisterConcrete(&MsgForceTransfer{}, "tokenfactory/force-transfer", nil)
	cdc.RegisterConcrete(&MsgChangeAdmin{}, "tokenfactory/change-admin", nil)
	cdc.RegisterConcrete(&AddCreatorsToDenomFeeWhitelistProposal{}, "tokenfactory/add-creators-to-denom-fee-whitelist-proposal", nil)
}

func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateDenom{},
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
	registry.RegisterImplementations((*govtypes.Content)(nil),
		&AddCreatorsToDenomFeeWhitelistProposal{},
	)
	// registry.RegisterImplementations((*govtypes.Content)(nil),
	// 	&MsgForceTransfer{},
	// )

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

var ModuleCdc = codec.NewProtoCodec(cdctypes.NewInterfaceRegistry())
