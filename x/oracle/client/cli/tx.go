package cli

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"github.com/sei-protocol/sei-chain/x/oracle/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/spf13/cobra"
)

// GetTxCmd returns the transaction commands for this module
func GetTxCmd() *cobra.Command {
	oracleTxCmd := &cobra.Command{
		Use:                        "oracle",
		Short:                      "Oracle transaction subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	oracleTxCmd.AddCommand(
		GetCmdDelegateFeederPermission(),
		GetCmdAggregateExchangeRatePrevote(),
		GetCmdAggregateExchangeRateVote(),
		GetCmdAggregateExchangeRateCombinedVote(),
	)

	return oracleTxCmd
}

// GetCmdDelegateFeederPermission will create a feeder permission delegation tx and sign it with the given key.
func GetCmdDelegateFeederPermission() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-feeder [feeder]",
		Args:  cobra.ExactArgs(1),
		Short: "Delegate the permission to vote for the oracle to an address",
		Long: strings.TrimSpace(`
Delegate the permission to submit exchange rate votes for the oracle to an address.

Delegation can keep your validator operator key offline and use a separate replaceable key online.

$ terrad tx oracle set-feeder terra1...

where "terra1..." is the address you want to delegate your voting rights to.
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			// Get from address
			voter := clientCtx.GetFromAddress()

			// The address the right is being delegated from
			validator := sdk.ValAddress(voter)

			feederStr := args[0]
			feeder, err := sdk.AccAddressFromBech32(feederStr)
			if err != nil {
				return err
			}

			msgs := []sdk.Msg{types.NewMsgDelegateFeedConsent(validator, feeder)}
			for _, msg := range msgs {
				if err := msg.ValidateBasic(); err != nil {
					return err
				}
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msgs...)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// GetCmdAggregateExchangeRatePrevote will create a aggregateExchangeRatePrevote tx and sign it with the given key.
func GetCmdAggregateExchangeRatePrevote() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aggregate-prevote [salt] [exchange-rates] [validator]",
		Args:  cobra.RangeArgs(2, 3),
		Short: "Submit an oracle aggregate prevote for the exchange rates of Luna",
		Long: strings.TrimSpace(`
Submit an oracle aggregate prevote for the exchange rates of Luna denominated in multiple denoms.
The purpose of aggregate prevote is to hide aggregate exchange rate vote with hash which is formatted
as hex string in SHA256("{salt}:{exchange_rate}{denom},...,{exchange_rate}{denom}:{voter}")

# Aggregate Prevote
$ terrad tx oracle aggregate-prevote 1234 8888.0ukrw,1.243uusd,0.99usdr

where "ukrw,uusd,usdr" is the denominating currencies, and "8888.0,1.243,0.99" is the exchange rates of micro Luna in micro denoms from the voter's point of view.

If voting from a voting delegate, set "validator" to the address of the validator to vote on behalf of:
$ terrad tx oracle aggregate-prevote 1234 8888.0ukrw,1.243uusd,0.99usdr terravaloper1...
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			salt := args[0]
			exchangeRatesStr := args[1]
			_, err = types.ParseExchangeRateTuples(exchangeRatesStr)
			if err != nil {
				return fmt.Errorf("given exchange_rates {%s} is not a valid format; exchange_rate should be formatted as DecCoins; %s", exchangeRatesStr, err.Error())
			}

			// Get from address
			voter := clientCtx.GetFromAddress()

			// By default the voter is voting on behalf of itself
			validator := sdk.ValAddress(voter)

			// Override validator if validator is given
			if len(args) == 3 {
				parsedVal, err := sdk.ValAddressFromBech32(args[2])
				if err != nil {
					return errors.Wrap(err, "validator address is invalid")
				}
				validator = parsedVal
			}

			hash := types.GetAggregateVoteHash(salt, exchangeRatesStr, validator)
			msgs := []sdk.Msg{types.NewMsgAggregateExchangeRatePrevote(hash, voter, validator)}
			for _, msg := range msgs {
				if err := msg.ValidateBasic(); err != nil {
					return err
				}
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msgs...)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// GetCmdAggregateExchangeRateVote will create a aggregateExchangeRateVote tx and sign it with the given key.
func GetCmdAggregateExchangeRateVote() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aggregate-vote [salt] [exchange-rates] [validator]",
		Args:  cobra.RangeArgs(2, 3),
		Short: "Submit an oracle aggregate vote for the exchange_rates of the base denom",
		Long: strings.TrimSpace(`
Submit a aggregate vote for the exchange_rates of the base denom w.r.t the input denom. Companion to a prevote submitted in the previous vote period.

$ seid tx oracle aggregate-vote 1234 8888.0ukrw,1.243uusd,0.99usdr

where "ukrw,uusd,usdr" is the denominating currencies, and "8888.0,1.243,0.99" is the exchange rates of micro Luna in micro denoms from the voter's point of view.

"salt" should match the salt used to generate the SHA256 hex in the aggregated pre-vote.

If voting from a voting delegate, set "validator" to the address of the validator to vote on behalf of:
$ seid tx oracle aggregate-vote 1234 8888.0ukrw,1.243uusd,0.99usdr seivaloper1....
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			salt := args[0]
			exchangeRatesStr := args[1]
			_, err = types.ParseExchangeRateTuples(exchangeRatesStr)
			if err != nil {
				return fmt.Errorf("given exchange_rate {%s} is not a valid format; exchange rate should be formatted as DecCoin; %s", exchangeRatesStr, err.Error())
			}

			// Get from address
			voter := clientCtx.GetFromAddress()

			// By default the voter is voting on behalf of itself
			validator := sdk.ValAddress(voter)

			// Override validator if validator is given
			if len(args) == 3 {
				parsedVal, err := sdk.ValAddressFromBech32(args[2])
				if err != nil {
					return errors.Wrap(err, "validator address is invalid")
				}
				validator = parsedVal
			}

			msgs := []sdk.Msg{types.NewMsgAggregateExchangeRateVote(salt, exchangeRatesStr, voter, validator)}
			for _, msg := range msgs {
				if err := msg.ValidateBasic(); err != nil {
					return err
				}
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msgs...)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// GetCmdAggregateExchangeRatePrevote will create a aggregateExchangeRatePrevote tx and sign it with the given key.
func GetCmdAggregateExchangeRateCombinedVote() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aggregate-combined-vote [vote-salt] [vote-exchange-rates] [prevote-salt] [prevote-exchange-rates] [validator]",
		Args:  cobra.RangeArgs(4, 5),
		Short: "Submit an oracle aggregate vote AND prevote for the exchange rates",
		Long: strings.TrimSpace(`
Submit an oracle aggregate vote and prevote for the exchange rates of the base denom denominated in multiple denoms. The vote is a companian to a prevote from the previous vote window.

The purpose of aggregate prevote is to hide aggregate exchange rate vote with hash which is formatted
as hex string in SHA256("{salt}:{exchange_rate}{denom},...,{exchange_rate}{denom}:{voter}")

# Aggregate Combined Vote
$ seid tx oracle aggregate-combined-vote 1234 8888.0ukrw,1.243uusd,0.99usdr 3456 9999.0ukrw,1.111uusd,0.95usdr

where "ukrw,uusd,usdr" is the denominating currencies, and "8888.0,1.243,0.99" is the exchange rates of micro base denom in micro denoms from the voter's point of view.

If voting from a voting delegate, set "validator" to the address of the validator to vote on behalf of:
$ terrad tx oracle aggregate-combined vote 1234 8888.0ukrw,1.243uusd,0.99usdr 3456 9999.0ukrw,1.111uusd,0.95usdr seivaloper1...
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			voteSalt := args[0]
			voteExchangeRatesStr := args[1]
			prevoteSalt := args[2]
			prevoteExchangeRatesStr := args[3]
			_, err = types.ParseExchangeRateTuples(voteExchangeRatesStr)
			if err != nil {
				return fmt.Errorf("given vote exchange_rates {%s} is not a valid format; exchange_rate should be formatted as DecCoins; %s", voteExchangeRatesStr, err.Error())
			}

			_, err = types.ParseExchangeRateTuples(prevoteExchangeRatesStr)
			if err != nil {
				return fmt.Errorf("given prevote exchange_rates {%s} is not a valid format; exchange_rate should be formatted as DecCoins; %s", prevoteExchangeRatesStr, err.Error())
			}

			// Get from address
			voter := clientCtx.GetFromAddress()

			// By default the voter is voting on behalf of itself
			validator := sdk.ValAddress(voter)

			// Override validator if validator is given
			if len(args) == 5 {
				parsedVal, err := sdk.ValAddressFromBech32(args[4])
				if err != nil {
					return errors.Wrap(err, "validator address is invalid")
				}
				validator = parsedVal
			}

			hash := types.GetAggregateVoteHash(prevoteSalt, prevoteExchangeRatesStr, validator)
			msgs := []sdk.Msg{types.NewMsgAggregateExchangeRateCombinedVote(voteSalt, voteExchangeRatesStr, hash, voter, validator)}
			for _, msg := range msgs {
				if err := msg.ValidateBasic(); err != nil {
					return err
				}
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msgs...)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
