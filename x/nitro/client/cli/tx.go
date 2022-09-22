package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"

	"github.com/sei-protocol/sei-chain/x/nitro/types"
)

// GetTxCmd returns the transaction commands for this module
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		NewRecordTransactionDataCmd(),
	)

	return cmd
}

func NewRecordTransactionDataCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "record-txs [slot] [root] [tx1,tx2...]",
		Short: "record nitro transactions and state root for a slot",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			slot, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return nil
			}
			txs := []string{}
			for i := 2; i < len(args); i++ {
				txs = append(txs, args[i])
			}

			txf := tx.NewFactoryCLI(clientCtx, cmd.Flags()).WithTxConfig(clientCtx.TxConfig).WithAccountRetriever(clientCtx.AccountRetriever)

			msg := types.NewMsgRecordTransactionData(
				clientCtx.GetFromAddress().String(),
				slot,
				args[1],
				txs,
			)

			return tx.GenerateOrBroadcastTxWithFactory(clientCtx, txf, msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
