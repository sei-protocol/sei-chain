package types

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	cdctypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
)

func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(
		&MsgUpdateResourceDependencyMappingProposal{},
		"cosmos-sdk/MsgUpdateResourceDependencyMappingProposal",
		nil,
	)
	cdc.RegisterConcrete(
		&MsgUpdateWasmDependencyMappingProposal{},
		"cosmos-sdk/MsgUpdateWasmDependencyMappingProposal",
		nil,
	)
}

func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*govtypes.Content)(nil),
		&MsgUpdateResourceDependencyMappingProposal{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterWasmDependency{},
	)
}

var ModuleCdc = codec.NewProtoCodec(cdctypes.NewInterfaceRegistry())
