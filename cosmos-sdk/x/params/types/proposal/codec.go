package proposal

import (
	"github.com/sei-protocol/sei-chain/cosmos-sdk/codec"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/codec/types"
	govtypes "github.com/sei-protocol/sei-chain/cosmos-sdk/x/gov/types"
)

// RegisterLegacyAminoCodec registers all necessary param module types with a given LegacyAmino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&ParameterChangeProposal{}, "cosmos-sdk/ParameterChangeProposal", nil)
}

func RegisterInterfaces(registry types.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*govtypes.Content)(nil),
		&ParameterChangeProposal{},
	)
}
