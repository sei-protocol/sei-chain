package types

import (
	"fmt"
	"strings"

	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

const (
	ProposalAddCreatorsToDenomFeeWhitelist = "AddAddCreatorsToDenomFeeWhitelist"
)

func init() {
	// for routing
	govtypes.RegisterProposalType(ProposalAddCreatorsToDenomFeeWhitelist)
	// for marshal and unmarshal
	govtypes.RegisterProposalTypeCodec(&AddCreatorsToDenomFeeWhitelistProposal{}, "tokenfactory/AddCreatorsToDenomFeeWhitelistProposal")
}

var _ govtypes.Content = &AddCreatorsToDenomFeeWhitelistProposal{}

func NewAddCreatorsToDenomFeeWhitelistProposal(title, description string, creatorList []string) AddCreatorsToDenomFeeWhitelistProposal {
	return AddCreatorsToDenomFeeWhitelistProposal{
		Title:       title,
		Description: description,
		CreatorList: creatorList,
	}
}

func (p *AddCreatorsToDenomFeeWhitelistProposal) GetTitle() string { return p.Title }

func (p *AddCreatorsToDenomFeeWhitelistProposal) GetDescription() string { return p.Description }

func (p *AddCreatorsToDenomFeeWhitelistProposal) ProposalRoute() string { return RouterKey }

func (p *AddCreatorsToDenomFeeWhitelistProposal) ProposalType() string {
	return ProposalAddCreatorsToDenomFeeWhitelist
}

func (p *AddCreatorsToDenomFeeWhitelistProposal) ValidateBasic() error {
	err := govtypes.ValidateAbstract(p)
	return err
}

// TODO: String support for add creators to denom fee whitelist type
func (p AddCreatorsToDenomFeeWhitelistProposal) String() string {
	creators := ""
	for _, creator := range p.CreatorList {
		creators += creator
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`Add Creators to Denom Fee Whitelist Proposal:
  Title:       %s
  Description: %s
  CreatorList: %s
`, p.Title, p.Description, creators))
	return b.String()
}
