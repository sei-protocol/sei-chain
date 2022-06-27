package types

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

const (
	ProposalTypeRegisterPairs = "RegisterPairs"
)

func init() {
	govtypes.RegisterProposalType(ProposalTypeRegisterPairs)
	govtypes.RegisterProposalTypeCodec(&RegisterPairsProposal{}, "dex/RegisterPairsProposal")
}

var _ govtypes.Content = &RegisterPairsProposal{}

func NewRegisterPairsProposal(title, description string, batchContractPair []BatchContractPair) RegisterPairsProposal {
	return RegisterPairsProposal{
		Title:       title,
		Description: description,
		BatchContractPair:    batchContractPair,
	}
}

func (p *RegisterPairsProposal) GetTitle() string { return p.Title }

func (p *RegisterPairsProposal) GetDescription() string { return p.Description }

func (p *RegisterPairsProposal) ProposalRoute() string { return RouterKey }

func (p *RegisterPairsProposal) ProposalType() string {
	return ProposalTypeRegisterPairs
}

// TODO: String support for register pair type
func (p RegisterPairsProposal) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`Update Fee Token Proposal:
  Title:       %s
  Description: %s
  Records:     %s
`, p.Title, p.Description, p.BatchContractPair.String()))
	return b.String()
}
