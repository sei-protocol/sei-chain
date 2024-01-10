package tx

import (
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

var _ = strconv.Itoa(0)

func CmdUnsuspendContract() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unsuspend-contract [contract address]",
		Short: "Unsuspend exchange contract",
		Long: strings.TrimSpace(`
			Unsuspend an exchange contract which was suspended due to Sudo malfunctioning, at a cost to its rent.
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			argContractAddr := args[0]

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := types.NewMsgUnsuspendContract(
				clientCtx.GetFromAddress().String(),
				argContractAddr,
			)

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
