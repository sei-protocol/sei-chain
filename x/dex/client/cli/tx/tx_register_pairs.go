package tx

import (
	"strconv"
	"strings"

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
		Long: strings.TrimSpace(`
			This allows for registering new pairs with a json file representing the various pairs to be registered. The pairs are specified within the file using the contract address for the orderbook along with pair information.
		`),
		Args: cobra.ExactArgs(1),
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

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
