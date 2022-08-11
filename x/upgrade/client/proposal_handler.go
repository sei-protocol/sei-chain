package client

import (
	govclient "github.com/cosmos/cosmos-sdk/x/gov/client"
	"github.com/sei-protocol/sei-chain/x/upgrade/client/cli"
	"github.com/sei-protocol/sei-chain/x/upgrade/client/rest"
)

var (
	ProposalHandler       = govclient.NewProposalHandler(cli.NewCmdSubmitUpgradeProposal, rest.ProposalRESTHandler)
	CancelProposalHandler = govclient.NewProposalHandler(cli.NewCmdSubmitCancelUpgradeProposal, rest.ProposalCancelRESTHandler)
)
