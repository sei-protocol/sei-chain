package cli

import (
	"context"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/x/oracle/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetQueryCmd returns the cli query commands for this module
func GetQueryCmd() *cobra.Command {
	oracleQueryCmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the oracle module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	oracleQueryCmd.AddCommand(
		GetCmdQueryExchangeRates(),
		GetCmdQueryPriceSnapshotHistory(),
		GetCmdQueryTwaps(),
		GetCmdQueryActives(),
		GetCmdQueryParams(),
		GetCmdQueryFeederDelegation(),
		GetCmdQueryVotePenaltyCounter(),
		GetCmdQueryAggregatePrevote(),
		GetCmdQueryAggregateVote(),
		GetCmdQueryVoteTargets(),
	)

	return oracleQueryCmd
}

// GetCmdQueryExchangeRates implements the query rate command.
func GetCmdQueryExchangeRates() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exchange-rates [denom]",
		Args:  cobra.RangeArgs(0, 1),
		Short: "Query the current Luna exchange rate w.r.t an asset",
		Long: strings.TrimSpace(`
Query the current exchange rate of Luna with an asset.
You can find the current list of active denoms by running

$ terrad query oracle exchange-rates

Or, can filter with denom

$ terrad query oracle exchange-rates ukrw
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			if len(args) == 0 {
				res, err := queryClient.ExchangeRates(context.Background(), &types.QueryExchangeRatesRequest{})
				if err != nil {
					return err
				}

				return clientCtx.PrintProto(res)
			}

			denom := args[0]
			res, err := queryClient.ExchangeRate(
				context.Background(),
				&types.QueryExchangeRateRequest{Denom: denom},
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

func GetCmdQueryPriceSnapshotHistory() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "price-snapshot-history",
		Args:  cobra.NoArgs,
		Short: "Query the history for oracle price snapshots",
		Long: strings.TrimSpace(`
Query the history for oracle price snapshots.
Example:

$ seid query oracle price-snapshot-history
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.PriceSnapshotHistory(context.Background(), &types.QueryPriceSnapshotHistoryRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func GetCmdQueryTwaps() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "twaps [lookback-seconds]",
		Args:  cobra.ExactArgs(1),
		Short: "Query the time weighted average prices for denoms with price snapshot data",
		Long: strings.TrimSpace(`
Query the time weighted average prices for denoms with price snapshot data
Example:

$ seid query oracle twaps
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			lookbackSeconds, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return err
			}

			res, err := queryClient.Twaps(
				context.Background(),
				&types.QueryTwapsRequest{LookbackSeconds: lookbackSeconds},
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

// GetCmdQueryActives implements the query actives command.
func GetCmdQueryActives() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "actives",
		Args:  cobra.NoArgs,
		Short: "Query the active list of Terra assets recognized by the oracle",
		Long: strings.TrimSpace(`
Query the active list of Terra assets recognized by the types.

$ terrad query oracle actives
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.Actives(context.Background(), &types.QueryActivesRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GetCmdQueryParams implements the query params command.
func GetCmdQueryParams() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Args:  cobra.NoArgs,
		Short: "Query the current Oracle params",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.Params(context.Background(), &types.QueryParamsRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GetCmdQueryFeederDelegation implements the query feeder delegation command
func GetCmdQueryFeederDelegation() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feeder [validator]",
		Args:  cobra.ExactArgs(1),
		Short: "Query the oracle feeder delegate account",
		Long: strings.TrimSpace(`
Query the account the validator's oracle voting right is delegated to.

$ terrad query oracle feeder terravaloper...
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			valString := args[0]
			validator, err := sdk.ValAddressFromBech32(valString)
			if err != nil {
				return err
			}

			res, err := queryClient.FeederDelegation(
				context.Background(),
				&types.QueryFeederDelegationRequest{ValidatorAddr: validator.String()},
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

// GetCmdQueryVotePenaltyCounter implements the query vote penalty counter of the validator command
func GetCmdQueryVotePenaltyCounter() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vote-penalty-counter [validator]",
		Args:  cobra.ExactArgs(1),
		Short: "Query the # of the miss count and abstain count",
		Long: strings.TrimSpace(`
Query the # of vote periods missed and abstained in this oracle slash window.

$ seid query oracle miss seivaloper...
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			valString := args[0]
			validator, err := sdk.ValAddressFromBech32(valString)
			if err != nil {
				return err
			}

			res, err := queryClient.VotePenaltyCounter(
				context.Background(),
				&types.QueryVotePenaltyCounterRequest{ValidatorAddr: validator.String()},
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

// GetCmdQueryAggregatePrevote implements the query aggregate prevote of the validator command
func GetCmdQueryAggregatePrevote() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aggregate-prevotes [validator]",
		Args:  cobra.RangeArgs(0, 1),
		Short: "Query outstanding oracle aggregate prevotes.",
		Long: strings.TrimSpace(`
Query outstanding oracle aggregate prevotes.

$ terrad query oracle aggregate-prevotes

Or, can filter with voter address

$ terrad query oracle aggregate-prevotes terravaloper...
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			if len(args) == 0 {
				res, err := queryClient.AggregatePrevotes(
					context.Background(),
					&types.QueryAggregatePrevotesRequest{},
				)
				if err != nil {
					return err
				}

				return clientCtx.PrintProto(res)
			}

			valString := args[0]
			validator, err := sdk.ValAddressFromBech32(valString)
			if err != nil {
				return err
			}

			res, err := queryClient.AggregatePrevote(
				context.Background(),
				&types.QueryAggregatePrevoteRequest{ValidatorAddr: validator.String()},
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

// GetCmdQueryAggregateVote implements the query aggregate prevote of the validator command
func GetCmdQueryAggregateVote() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aggregate-votes [validator]",
		Args:  cobra.RangeArgs(0, 1),
		Short: "Query outstanding oracle aggregate votes.",
		Long: strings.TrimSpace(`
Query outstanding oracle aggregate vote.

$ terrad query oracle aggregate-votes

Or, can filter with voter address

$ terrad query oracle aggregate-votes terravaloper...
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			if len(args) == 0 {
				res, err := queryClient.AggregateVotes(
					context.Background(),
					&types.QueryAggregateVotesRequest{},
				)
				if err != nil {
					return err
				}

				return clientCtx.PrintProto(res)
			}

			valString := args[0]
			validator, err := sdk.ValAddressFromBech32(valString)
			if err != nil {
				return err
			}

			res, err := queryClient.AggregateVote(
				context.Background(),
				&types.QueryAggregateVoteRequest{ValidatorAddr: validator.String()},
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

// GetCmdQueryVoteTargets implements the query params command.
func GetCmdQueryVoteTargets() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vote-targets",
		Args:  cobra.NoArgs,
		Short: "Query the current Oracle vote targets",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.VoteTargets(
				context.Background(),
				&types.QueryVoteTargetsRequest{},
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
