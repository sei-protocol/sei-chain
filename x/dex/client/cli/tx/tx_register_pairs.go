package tx

import (
	"strconv"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	cutils "github.com/sei-protocol/sei-chain/x/dex/client/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

var _ = strconv.Itoa(0)

func CmdRegisterPairs() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-pairs [register-pairs-file]",
		Short: "Register pairs for a contract",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			registerTx, err := cutils.ParseRegisterPairsTxJSON(clientCtx.LegacyAmino, args[0])
			if err != nil {
				return err
			}

			txBatchContractPair, err := registerTx.BatchContractPair.ToMultipleBatchContractPair()
			if err != nil {
				return err
			}

			msg := types.NewMsgRegisterPairs(
				clientCtx.GetFromAddress().String(),
				txBatchContractPair,
			)
			if err := msg.ValidateBasic(); err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
