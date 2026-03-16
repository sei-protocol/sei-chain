package client

import (
	govclient "github.com/sei-protocol/sei-chain/cosmos/x/gov/client"
	"github.com/sei-protocol/sei-chain/cosmos/x/params/client/cli"
	"github.com/sei-protocol/sei-chain/cosmos/x/params/client/rest"
)

// ProposalHandler is the param change proposal handler.
var ProposalHandler = govclient.NewProposalHandler(cli.NewSubmitParamChangeProposalTxCmd, rest.ProposalRESTHandler)
