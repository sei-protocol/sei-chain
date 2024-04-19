package cli

import (
	"github.com/spf13/cobra"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
)

// NewQueryCmd returns a root CLI command handler for all x/params query commands.
func NewQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the params module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(NewQuerySubspaceParamsCmd())
	cmd.AddCommand(NewQueryFeeParamsCmd())
	cmd.AddCommand(NewQueryCosmosGasParamsCmd())
	cmd.AddCommand(NewQueryBlockParamsCmd())

	return cmd
}

// NewQuerySubspaceParamsCmd returns a CLI command handler for querying subspace
// parameters managed by the x/params module.
func NewQuerySubspaceParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "subspace [subspace] [key]",
		Short: "Query for raw parameters by subspace and key",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := proposal.NewQueryClient(clientCtx)

			params := proposal.QueryParamsRequest{Subspace: args[0], Key: args[1]}
			res, err := queryClient.Params(cmd.Context(), &params)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(&res.Param)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func NewQueryFeeParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feesparams",
		Short: "Query for fee params",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := proposal.NewQueryClient(clientCtx)

			params := proposal.QueryParamsRequest{Subspace: "params", Key: string(types.ParamStoreKeyFeesParams)}
			res, err := queryClient.Params(cmd.Context(), &params)
			if err != nil {
				return err
			}

			feeParams := types.FeesParams{}
			clientCtx.Codec.UnmarshalJSON([]byte(res.Param.Value), &feeParams)

			return clientCtx.PrintProto(&feeParams)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func NewQueryCosmosGasParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cosmosgasparams",
		Short: "Query for cosmos gas params",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := proposal.NewQueryClient(clientCtx)

			params := proposal.QueryParamsRequest{Subspace: "params", Key: string(types.ParamStoreKeyCosmosGasParams)}
			res, err := queryClient.Params(cmd.Context(), &params)
			if err != nil {
				return err
			}

			cosmosGasParams := types.CosmosGasParams{}
			clientCtx.Codec.UnmarshalJSON([]byte(res.Param.Value), &cosmosGasParams)

			return clientCtx.PrintProto(&cosmosGasParams)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func NewQueryBlockParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blockparams",
		Short: "Query for block params",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := proposal.NewQueryClient(clientCtx)

			params := proposal.QueryParamsRequest{Subspace: "baseapp", Key: string(baseapp.ParamStoreKeyBlockParams)}
			res, err := queryClient.Params(cmd.Context(), &params)
			if err != nil {
				return err
			}

			blockParams := tmproto.BlockParams{}
			clientCtx.Codec.UnmarshalJSON([]byte(res.Param.Value), &blockParams)

			return clientCtx.PrintProto(&blockParams)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
