package query

import (
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

var _ = strconv.Itoa(0)

func CmdGetTwaps() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-twaps [contract-address] [lookback]",
		Short: "Query getPrice",
		Long: strings.TrimSpace(`
			Get the TWAPs (Time weighted average prices) for all registered dex pairs for a specific orderbook by [contract-address] with a specific lookback duration over which to compute the weighted average.
		`),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			reqContractAddr := args[0]
			reqLookback, err := strconv.ParseUint(args[1], 10, 64)
			if err != nil {
				return err
			}

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			params := &types.QueryGetTwapsRequest{
				ContractAddr:    reqContractAddr,
				LookbackSeconds: reqLookback,
			}

			res, err := queryClient.GetTwaps(cmd.Context(), params)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
