package cli

import (
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
)

// ParseCommunityPoolSpendProposalWithDeposit reads and parses a CommunityPoolSpendProposalWithDeposit from a file.
func ParseCommunityPoolSpendProposalWithDeposit(cdc codec.JSONCodec, proposalFile string) (types.CommunityPoolSpendProposalWithDeposit, error) {
	proposal := types.CommunityPoolSpendProposalWithDeposit{}

	contents, err := os.ReadFile(filepath.Clean(proposalFile))
	if err != nil {
		return proposal, err
	}

	if err = cdc.UnmarshalAsJSON(contents, &proposal); err != nil {
		return proposal, err
	}

	return proposal, nil
}
