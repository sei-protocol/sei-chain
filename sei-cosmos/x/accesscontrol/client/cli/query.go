package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("Querying commands for the %s module", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		GetParams(),
		GetResourceDependencyMapping(),
		ListResourceDependencyMapping(),
		GetWasmDependencyAccessOps(),
		ListWasmDependencyMapping(),
	)

	return cmd
}

// GetParams returns the params for the module
func GetParams() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params [flags]",
		Short: "Get the params for the x/accesscontrol module",
		Long:  "Get the params for the x/accesscontrol module",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.Params(cmd.Context(), &types.QueryParamsRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func GetResourceDependencyMapping() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resource-dependency-mapping [messageKey] [flags]",
		Short: "Get the resource dependency mapping for a specific message key",
		Long: "Get the resource dependency mapping for a specific message key. E.g.\n" +
			"$ seid q accesscontrol resource-dependency-mapping [messageKey] [flags]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.ResourceDependencyMappingFromMessageKey(
				cmd.Context(),
				&types.ResourceDependencyMappingFromMessageKeyRequest{MessageKey: args[0]},
			)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func ListResourceDependencyMapping() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-resource-dependency-mapping [flags]",
		Short: "List all resource dependency mappings",
		Long:  "List all resource dependency mappings",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.ListResourceDependencyMapping(
				cmd.Context(),
				&types.ListResourceDependencyMappingRequest{},
			)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func GetWasmDependencyAccessOps() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wasm-dependency-mapping [contractAddr] [flags]",
		Short: "Get the wasm contract dependency mapping for a specific contract address",
		Long: "Get the wasm contract dependency mapping for a specific contract address. E.g.\n" +
			"$ seid q accesscontrol wasm-dependency-mapping [contractAddr] [flags]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.WasmDependencyMapping(
				cmd.Context(),
				&types.WasmDependencyMappingRequest{ContractAddress: args[0]},
			)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func ListWasmDependencyMapping() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-wasm-dependency-mapping [flags]",
		Short: "List all wasm contract dependency mappings",
		Long:  "List all wasm contract dependency mappings",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.ListWasmDependencyMapping(
				cmd.Context(),
				&types.ListWasmDependencyMappingRequest{},
			)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
