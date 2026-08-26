// Package gov provides decode-only compatibility for retired IBC governance proposals.
package gov

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/app/retiredibc"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
)

const (
	RouterKey                = "client"
	ProposalTypeClientUpdate = "ClientUpdate"
	ProposalTypeUpgrade      = "IBCUpgrade"

	ClientUpdateProposalTypeURL = "/ibc.core.client.v1.ClientUpdateProposal"
	UpgradeProposalTypeURL      = "/ibc.core.client.v1.UpgradeProposal"
)

var (
	_ govtypes.Content                   = (*ClientUpdateProposal)(nil)
	_ govtypes.Content                   = (*UpgradeProposal)(nil)
	_ codectypes.UnpackInterfacesMessage = (*UpgradeProposal)(nil)
)

func init() {
	govtypes.RegisterProposalType(ProposalTypeClientUpdate)
	govtypes.RegisterProposalType(ProposalTypeUpgrade)
}

func (p *ClientUpdateProposal) GetTitle() string       { return p.Title }
func (p *ClientUpdateProposal) GetDescription() string { return p.Description }
func (*ClientUpdateProposal) ProposalRoute() string    { return RouterKey }
func (*ClientUpdateProposal) ProposalType() string     { return ProposalTypeClientUpdate }
func (*ClientUpdateProposal) ValidateBasic() error     { return retiredibc.ErrDeprecated }

func (p *UpgradeProposal) GetTitle() string       { return p.Title }
func (p *UpgradeProposal) GetDescription() string { return p.Description }
func (*UpgradeProposal) ProposalRoute() string    { return RouterKey }
func (*UpgradeProposal) ProposalType() string     { return ProposalTypeUpgrade }
func (*UpgradeProposal) ValidateBasic() error     { return retiredibc.ErrDeprecated }

// String returns the text representation of the retired upgrade proposal.
func (p *UpgradeProposal) String() string {
	return fmt.Sprintf("IBC Upgrade Proposal: %s - %s", p.Title, p.Description)
}

// UnpackInterfaces leaves the retired upgraded client state opaque.
func (*UpgradeProposal) UnpackInterfaces(codectypes.AnyUnpacker) error {
	return nil
}
