package cli

import (
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/spf13/cobra"

	bankprecompilequery "github.com/sei-protocol/sei-chain/precompiles/bank/query"
	precompilequery "github.com/sei-protocol/sei-chain/precompiles/query"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/version"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
)

const (
	FlagDenom              = "denom"
	FlagEVMRPC             = "evm-rpc"
	FlagQueryClientBackend = "query-client-backend"
	defaultEVMRPCURL       = "http://localhost:8545"
	QueryClientPrecompile  = "precompile"
	QueryClientLegacy      = "legacy"
)

// GetQueryCmd returns the parent command for all x/bank CLi query commands. The
// provided clientCtx should have, at a minimum, a verifier, Tendermint RPC client,
// and marshaler set.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the bank module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		GetBalancesCmd(),
		GetCmdQueryTotalSupply(),
		GetCmdDenomsMetadata(),
	)

	return cmd
}

func GetBalancesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "balances [address]",
		Short: "Query for account balances by address",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Query the total balance of an account or of a specific denomination.

Example:
  $ %s query %s balances [address]
  $ %s query %s balances [address] --denom=[denom]
`,
				version.AppName, types.ModuleName, version.AppName, types.ModuleName,
			),
		),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			denom, err := cmd.Flags().GetString(FlagDenom)
			if err != nil {
				return err
			}

			queryClient, closeQueryClient, err := NewQueryClient(cmd, clientCtx)
			if err != nil {
				return err
			}
			defer closeQueryClient()

			addr, err := sdk.AccAddressFromBech32(args[0])
			if err != nil {
				return err
			}

			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if denom == "" {
				params := types.NewQueryAllBalancesRequest(addr, pageReq)
				res, err := queryClient.AllBalances(ctx, params)
				if err != nil {
					return err
				}
				return clientCtx.PrintProto(res)
			}

			params := types.NewQueryBalanceRequest(addr, denom)
			res, err := queryClient.Balance(ctx, params)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res.Balance)
		},
	}

	cmd.Flags().String(FlagDenom, "", "The specific balance denomination to query for")
	addEVMRPCFlag(cmd)
	addQueryClientBackendFlag(cmd)
	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "all balances")

	return cmd
}

// GetCmdDenomsMetadata defines the cobra command to query client denomination metadata.
func GetCmdDenomsMetadata() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "denom-metadata",
		Short: "Query the client metadata for coin denominations",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Query the client metadata for all the registered coin denominations

Example:
  To query for the client metadata of all coin denominations use:
  $ %s query %s denom-metadata

To query for the client metadata of a specific coin denomination use:
  $ %s query %s denom-metadata --denom=[denom]
`,
				version.AppName, types.ModuleName, version.AppName, types.ModuleName,
			),
		),
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			denom, err := cmd.Flags().GetString(FlagDenom)
			if err != nil {
				return err
			}

			queryClient, closeQueryClient, err := NewQueryClient(cmd, clientCtx)
			if err != nil {
				return err
			}
			defer closeQueryClient()

			if denom == "" {
				res, err := queryClient.DenomsMetadata(cmd.Context(), &types.QueryDenomsMetadataRequest{})
				if err != nil {
					return err
				}

				return clientCtx.PrintProto(res)
			}

			res, err := queryClient.DenomMetadata(cmd.Context(), &types.QueryDenomMetadataRequest{Denom: denom})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	cmd.Flags().String(FlagDenom, "", "The specific denomination to query client metadata for")
	addEVMRPCFlag(cmd)
	addQueryClientBackendFlag(cmd)
	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func GetCmdQueryTotalSupply() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "total",
		Short: "Query the total supply of coins of the chain",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Query total supply of coins that are held by accounts in the chain.

Example:
  $ %s query %s total

To query for the total supply of a specific coin denomination use:
  $ %s query %s total --denom=[denom]
`,
				version.AppName, types.ModuleName, version.AppName, types.ModuleName,
			),
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			denom, err := cmd.Flags().GetString(FlagDenom)
			if err != nil {
				return err
			}

			queryClient, closeQueryClient, err := NewQueryClient(cmd, clientCtx)
			if err != nil {
				return err
			}
			defer closeQueryClient()
			ctx := cmd.Context()

			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}
			if denom == "" {
				res, err := queryClient.TotalSupply(ctx, &types.QueryTotalSupplyRequest{Pagination: pageReq})
				if err != nil {
					return err
				}

				return clientCtx.PrintProto(res)
			}

			res, err := queryClient.SupplyOf(ctx, &types.QuerySupplyOfRequest{Denom: denom})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(&res.Amount)
		},
	}

	cmd.Flags().String(FlagDenom, "", "The specific balance denomination to query for")
	addEVMRPCFlag(cmd)
	addQueryClientBackendFlag(cmd)
	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "all supply totals")

	return cmd
}

func NewQueryClient(cmd *cobra.Command, clientCtx client.Context) (types.QueryClient, func(), error) {
	backend, err := cmd.Flags().GetString(FlagQueryClientBackend)
	if err != nil {
		return nil, nil, err
	}
	switch backend {
	case QueryClientPrecompile:
		return NewPrecompileBackedQueryClient(cmd, clientCtx)
	case QueryClientLegacy:
		return types.NewQueryClient(clientCtx), func() {}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported bank query client backend %q", backend)
	}
}

func NewPrecompileBackedQueryClient(cmd *cobra.Command, clientCtx client.Context) (types.QueryClient, func(), error) {
	rpcURL, err := cmd.Flags().GetString(FlagEVMRPC)
	if err != nil {
		return nil, nil, err
	}
	evmClient, err := ethclient.DialContext(cmd.Context(), rpcURL)
	if err != nil {
		return nil, nil, err
	}
	return types.NewQueryClient(precompilequery.NewConn(
		evmClient,
		bankprecompilequery.Registry(),
		precompilequery.WithDefaultBlockNumber(clientCtx.Height),
	)), evmClient.Close, nil
}

func addEVMRPCFlag(cmd *cobra.Command) {
	cmd.Flags().String(FlagEVMRPC, defaultEVMRPCURL, "EVM RPC endpoint for precompile-backed bank queries")
}

func addQueryClientBackendFlag(cmd *cobra.Command) {
	cmd.Flags().String(FlagQueryClientBackend, QueryClientPrecompile, "bank query client backend")
	_ = cmd.Flags().MarkHidden(FlagQueryClientBackend)
}
