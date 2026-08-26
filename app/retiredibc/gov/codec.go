package gov

import (
	"github.com/sei-protocol/sei-chain/app/retiredibc"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
)

// RegisterInterfaces registers only the retired IBC governance content needed to decode state.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*govtypes.Content)(nil),
		&ClientUpdateProposal{},
		&UpgradeProposal{},
	)
}

// ProposalHandler fails execution of a retired IBC proposal.
func ProposalHandler(sdk.Context, govtypes.Content) error {
	return retiredibc.ErrDeprecated
}
