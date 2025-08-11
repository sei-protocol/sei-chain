package client

import (
	govclient "github.com/sei-protocol/sei-chain/cosmos-sdk/x/gov/client"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/x/params/client/cli"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/x/params/client/rest"
)

// ProposalHandler is the param change proposal handler.
var ProposalHandler = govclient.NewProposalHandler(cli.NewSubmitParamChangeProposalTxCmd, rest.ProposalRESTHandler)
