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
		GetCmdAggregateExchangeRateVote(),
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

$ seid tx oracle set-feeder terra1...

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

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msgs...)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// GetCmdAggregateExchangeRateVote will create a aggregateExchangeRateVote tx and sign it with the given key.
func GetCmdAggregateExchangeRateVote() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aggregate-vote [exchange-rates] [validator]",
		Args:  cobra.RangeArgs(1, 2),
		Short: "Submit an oracle aggregate vote for the exchange_rates of the base denom",
		Long: strings.TrimSpace(`
Submit a aggregate vote for the exchange_rates of the base denom w.r.t the input denom.

$ seid tx oracle aggregate-vote 8888.0ukrw,1.243uusd,0.99usdr

where "ukrw,uusd,usdr" is the denominating currencies, and "8888.0,1.243,0.99" is the exchange rates of micro USD in micro denoms from the voter's point of view.

If voting from a voting delegate, set "validator" to the address of the validator to vote on behalf of:
$ seid tx oracle aggregate-vote 1234 8888.0ukrw,1.243uusd,0.99usdr seivaloper1....
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			exchangeRatesStr := args[0]
			_, err = types.ParseExchangeRateTuples(exchangeRatesStr)
			if err != nil {
				return fmt.Errorf("given exchange_rate {%s} is not a valid format; exchange rate should be formatted as DecCoin; %s", exchangeRatesStr, err.Error())
			}

			// Get from address
			voter := clientCtx.GetFromAddress()

			// By default the voter is voting on behalf of itself
			validator := sdk.ValAddress(voter)

			// Override validator if validator is given
			if len(args) == 2 {
				parsedVal, err := sdk.ValAddressFromBech32(args[1])
				if err != nil {
					return errors.Wrap(err, "validator address is invalid")
				}
				validator = parsedVal
			}

			msgs := []sdk.Msg{types.NewMsgAggregateExchangeRateVote(exchangeRatesStr, voter, validator)}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msgs...)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
