package client

import (
	"github.com/sei-protocol/sei-chain/cosmos-sdk/x/accesscontrol/client/cli"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/x/accesscontrol/client/rest"
	govclient "github.com/sei-protocol/sei-chain/cosmos-sdk/x/gov/client"
)

var ResourceDependencyProposalHandler = govclient.NewProposalHandler(cli.MsgUpdateResourceDependencyMappingProposalCmd, rest.UpdateResourceDependencyProposalRESTHandler)
