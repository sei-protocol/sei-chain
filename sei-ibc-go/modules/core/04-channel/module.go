package channel

import (
	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/04-channel/client/cli"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/04-channel/types"
)

// Name returns the IBC channel ICS name.
func Name() string {
	return types.SubModuleName
}

// GetTxCmd returns the root tx command for IBC channels.
func GetTxCmd() *cobra.Command {
	return cli.NewTxCmd()
}
