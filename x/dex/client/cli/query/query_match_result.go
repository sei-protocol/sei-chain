package query

import (
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

func CmdGetMatchResult() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-match-result [contract-address]",
		Short: "Query get match result by contract",
		Long: strings.TrimSpace(`
			Gets the match result information for an orderbook specified by the given contract address. The match result information includes the orders, settlements, and cancellations for the orderbook.
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			contractAddr := args[0]
			if err != nil {
				return err
			}

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			params := &types.QueryGetMatchResultRequest{
				ContractAddr: contractAddr,
			}

			res, err := queryClient.GetMatchResult(cmd.Context(), params)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
