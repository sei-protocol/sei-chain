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

func CmdGetRegisteredPairs() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-registered-pairs [contract address]",
		Short: "Query Registered Pairs",
		Long: strings.TrimSpace(`
			List all of the registered pairs for an orderbook specified by contract address.
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			contractAddr := args[0]
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			params := &types.QueryRegisteredPairsRequest{
				ContractAddr: contractAddr,
			}

			res, err := queryClient.GetRegisteredPairs(cmd.Context(), params)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
