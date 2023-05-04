package types

import (
	"fmt"
	"strings"

	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

const (
	ProposalTypeUpdateMinter = "UpdateMinter"
)

func init() {
	// for routing
	govtypes.RegisterProposalType(ProposalTypeUpdateMinter)
	// for marshal and unmarshal
	govtypes.RegisterProposalTypeCodec(&UpdateMinterProposal{}, "mint/UpdateMinterProposal")
}

func (p *UpdateMinterProposal) GetTitle() string { return p.Title }

func (p *UpdateMinterProposal) GetDescription() string { return p.Description }

func (p *UpdateMinterProposal) ProposalRoute() string { return RouterKey }

func (p *UpdateMinterProposal) ProposalType() string {
	return ProposalTypeUpdateMinter
}

func (p *UpdateMinterProposal) ValidateBasic() error {
	return ValidateMinter(*p.Minter)
}

func (p UpdateMinterProposal) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`Update Minter Proposal:
  Title:       %s
  Description: %s
  Minter:     %s
`, p.Title, p.Description, p.Minter.String()))
	return b.String()
}

func NewUpdateMinterProposalHandler(title, description string, minter Minter) *UpdateMinterProposal {
	return &UpdateMinterProposal{title, description, &minter}
}
