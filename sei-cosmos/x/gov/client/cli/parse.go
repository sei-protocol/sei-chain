package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/pflag"

	govutils "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/client/utils"
)

func parseSubmitProposalFlags(fs *pflag.FlagSet) (*proposal, error) {
	proposal := &proposal{}
	proposalFile, err := fs.GetString(FlagProposal)
	if err != nil {
		return nil, err
	}

	if proposalFile == "" {
		proposalType, _ := fs.GetString(FlagProposalType)

		proposal.Title, _ = fs.GetString(FlagTitle)
		proposal.Description, _ = fs.GetString(FlagDescription)
		proposal.Type = govutils.NormalizeProposalType(proposalType)
		proposal.Deposit, _ = fs.GetString(FlagDeposit)
		proposal.IsExpedited, _ = fs.GetBool(FlagIsExpedited)
		return proposal, nil
	}

	for _, flag := range ProposalFlags {
		if v, _ := fs.GetString(flag); v != "" {
			return nil, fmt.Errorf("--%s flag provided alongside --proposal, which is a noop", flag)
		}
	}
	proposalFile = filepath.Clean(proposalFile)
	contents, err := os.ReadFile(proposalFile)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(contents, proposal)
	if err != nil {
		return nil, err
	}

	return proposal, nil
}
