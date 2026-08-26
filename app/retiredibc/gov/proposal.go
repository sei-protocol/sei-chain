// Package gov provides decode-only compatibility for retired IBC governance proposals.
package gov

import (
	"encoding/json"
	"fmt"

	"github.com/gogo/protobuf/jsonpb"

	"github.com/sei-protocol/sei-chain/app/retiredibc"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
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

// ValidateProposalSubmission rejects new retired IBC client update proposals.
func (*ClientUpdateProposal) ValidateProposalSubmission() error {
	return retiredibc.ErrDeprecated
}

func (p *UpgradeProposal) GetTitle() string       { return p.Title }
func (p *UpgradeProposal) GetDescription() string { return p.Description }
func (*UpgradeProposal) ProposalRoute() string    { return RouterKey }
func (*UpgradeProposal) ProposalType() string     { return ProposalTypeUpgrade }
func (*UpgradeProposal) ValidateBasic() error     { return retiredibc.ErrDeprecated }

// ValidateProposalSubmission rejects new retired IBC upgrade proposals.
func (*UpgradeProposal) ValidateProposalSubmission() error {
	return retiredibc.ErrDeprecated
}

// String returns the text representation of the retired upgrade proposal.
func (p *UpgradeProposal) String() string {
	return fmt.Sprintf("IBC Upgrade Proposal: %s - %s", p.Title, p.Description)
}

// UnpackInterfaces leaves the retired upgraded client state opaque.
func (*UpgradeProposal) UnpackInterfaces(codectypes.AnyUnpacker) error {
	return nil
}

type opaqueAnyJSON struct {
	TypeURL string `json:"type_url"`
	Value   []byte `json:"value"`
}

type upgradeProposalJSON struct {
	Title               string            `json:"title"`
	Description         string            `json:"description"`
	Plan                upgradetypes.Plan `json:"plan"`
	UpgradedClientState *opaqueAnyJSON    `json:"upgraded_client_state,omitempty"`
}

// MarshalJSONPB encodes the nested retired client state as an opaque Any.
func (p *UpgradeProposal) MarshalJSONPB(*jsonpb.Marshaler) ([]byte, error) {
	proposal := upgradeProposalJSON{
		Title:       p.Title,
		Description: p.Description,
		Plan:        p.Plan,
	}
	if p.UpgradedClientState != nil {
		proposal.UpgradedClientState = &opaqueAnyJSON{
			TypeURL: p.UpgradedClientState.TypeUrl,
			Value:   p.UpgradedClientState.Value,
		}
	}
	return json.Marshal(proposal)
}

// UnmarshalJSONPB decodes the nested retired client state as an opaque Any.
func (p *UpgradeProposal) UnmarshalJSONPB(_ *jsonpb.Unmarshaler, data []byte) error {
	var proposal upgradeProposalJSON
	if err := json.Unmarshal(data, &proposal); err != nil {
		return err
	}
	p.Title = proposal.Title
	p.Description = proposal.Description
	p.Plan = proposal.Plan
	if proposal.UpgradedClientState == nil {
		p.UpgradedClientState = nil
	} else {
		p.UpgradedClientState = &codectypes.Any{
			TypeUrl: proposal.UpgradedClientState.TypeURL,
			Value:   proposal.UpgradedClientState.Value,
		}
	}
	return nil
}
