package query

import (
	"strconv"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

func CmdGetMatchResult() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-match-result [contract-address] [height]",
		Short: "Query get match result by contract and height",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			contractAddr := args[0]
			height, err := strconv.ParseInt(args[1], 10, 64)
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
				Height:       height,
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
