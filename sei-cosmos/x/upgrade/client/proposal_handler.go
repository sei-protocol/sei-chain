package client

import (
	govclient "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/client/cli"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/client/rest"
)

var ProposalHandler = govclient.NewProposalHandler(cli.NewCmdSubmitUpgradeProposal, rest.ProposalRESTHandler)
var CancelProposalHandler = govclient.NewProposalHandler(cli.NewCmdSubmitCancelUpgradeProposal, rest.ProposalCancelRESTHandler)
