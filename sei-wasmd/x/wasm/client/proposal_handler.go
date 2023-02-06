package client

import (
	govclient "github.com/cosmos/cosmos-sdk/x/gov/client"

	"github.com/CosmWasm/wasmd/x/wasm/client/cli"
	"github.com/CosmWasm/wasmd/x/wasm/client/rest"
)

// ProposalHandlers define the wasm cli proposal types and rest handler.
var ProposalHandlers = []govclient.ProposalHandler{
	govclient.NewProposalHandler(cli.ProposalStoreCodeCmd, rest.StoreCodeProposalHandler),
	govclient.NewProposalHandler(cli.ProposalInstantiateContractCmd, rest.InstantiateProposalHandler),
	govclient.NewProposalHandler(cli.ProposalMigrateContractCmd, rest.MigrateProposalHandler),
	govclient.NewProposalHandler(cli.ProposalExecuteContractCmd, rest.ExecuteProposalHandler),
	govclient.NewProposalHandler(cli.ProposalSudoContractCmd, rest.SudoProposalHandler),
	govclient.NewProposalHandler(cli.ProposalUpdateContractAdminCmd, rest.UpdateContractAdminProposalHandler),
	govclient.NewProposalHandler(cli.ProposalClearContractAdminCmd, rest.ClearContractAdminProposalHandler),
	govclient.NewProposalHandler(cli.ProposalPinCodesCmd, rest.PinCodeProposalHandler),
	govclient.NewProposalHandler(cli.ProposalUnpinCodesCmd, rest.UnpinCodeProposalHandler),
	govclient.NewProposalHandler(cli.ProposalUpdateInstantiateConfigCmd, rest.UpdateInstantiateConfigProposalHandler),
}
