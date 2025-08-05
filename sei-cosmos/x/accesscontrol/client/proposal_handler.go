package client

import (
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/client/cli"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/client/rest"
	govclient "github.com/cosmos/cosmos-sdk/x/gov/client"
)

var ResourceDependencyProposalHandler = govclient.NewProposalHandler(cli.MsgUpdateResourceDependencyMappingProposalCmd, rest.UpdateResourceDependencyProposalRESTHandler)
