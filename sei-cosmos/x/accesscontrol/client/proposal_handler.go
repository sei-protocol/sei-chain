package client

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/accesscontrol/client/cli"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/accesscontrol/client/rest"
	govclient "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/client"
)

var ResourceDependencyProposalHandler = govclient.NewProposalHandler(cli.MsgUpdateResourceDependencyMappingProposalCmd, rest.UpdateResourceDependencyProposalRESTHandler)
