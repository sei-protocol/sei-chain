package types

import (
	"fmt"
	"strings"

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
		Batchcontractpair:    batchContractPair,
	}
}

func (p *RegisterPairsProposal) GetTitle() string { return p.Title }

func (p *RegisterPairsProposal) GetDescription() string { return p.Description }

func (p *RegisterPairsProposal) ProposalRoute() string { return RouterKey }

func (p *RegisterPairsProposal) ProposalType() string {
	return ProposalTypeRegisterPairs
}

func (p *RegisterPairsProposal) ValidateBasic() error {
	err := govtypes.ValidateAbstract(p)
	return err
}

// TODO: String support for register pair type
func (p RegisterPairsProposal) String() string {
	batchContractPairRecords := ""
	for _, contractPair := range p.Batchcontractpair {
		batchContractPairRecords += contractPair.String()
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`Update Fee Token Proposal:
  Title:       %s
  Description: %s
  Records:     %s
`, p.Title, p.Description, batchContractPairRecords))
	return b.String()
}
