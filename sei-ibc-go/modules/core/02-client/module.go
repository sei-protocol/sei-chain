package client

import (
	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/client/cli"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
)

// Name returns the IBC client name
func Name() string {
	return types.SubModuleName
}

// GetTxCmd returns the root tx command for 02-client.
func GetTxCmd() *cobra.Command {
	return cli.NewTxCmd()
}
