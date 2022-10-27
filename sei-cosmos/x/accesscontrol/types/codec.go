package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	// this line is used by starport scaffolding # 1
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
	registry.RegisterImplementations((*govtypes.Content)(nil),
		&MsgUpdateWasmDependencyMappingProposal{},
	)
}

var ModuleCdc = codec.NewProtoCodec(cdctypes.NewInterfaceRegistry())
