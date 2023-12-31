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

func CmdUpdatePriceTickSize() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-price-tick-size [update-price-tick-size-file]",
		Short: "Update price tick size for a market",
		Long: strings.TrimSpace(`
			Updates the price tick size for a specific pair for an orderbook specified by contract address. The file contains a list of pair info, new tick size, and contract addresses to allow for updating multiple tick sizes in one transaction.
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			tickTx, err := cutils.ParseUpdateTickSizeTxJSON(clientCtx.LegacyAmino, args[0])
			if err != nil {
				return err
			}

			txTick, err := tickTx.TickSizes.ToTickSizes()
			if err != nil {
				return err
			}

			msg := types.NewMsgUpdatePriceTickSize(
				clientCtx.GetFromAddress().String(),
				txTick,
			)

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
