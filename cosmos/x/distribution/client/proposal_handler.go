package client

import (
	"github.com/sei-protocol/sei-chain/cosmos/x/distribution/client/cli"
	"github.com/sei-protocol/sei-chain/cosmos/x/distribution/client/rest"
	govclient "github.com/sei-protocol/sei-chain/cosmos/x/gov/client"
)

// ProposalHandler is the community spend proposal handler.
var (
	ProposalHandler = govclient.NewProposalHandler(cli.GetCmdSubmitProposal, rest.ProposalRESTHandler)
)
