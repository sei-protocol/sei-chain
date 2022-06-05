package cli

import (
	"errors"
	"strconv"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cobra"
)

var _ = strconv.Itoa(0)

func CmdRegisterPair() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-pair [contract address] [price denom] [asset denom]",
		Short: "Register tradable pair",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			argContractAddr := args[0]
			reqPriceDenom, unit, err := types.GetDenomFromStr(args[1])
			if err != nil {
				return err
			}
			if unit != types.Unit_STANDARD {
				return errors.New("Denom must be in standard/whole unit (e.g. sei instead of usei)")
			}
			reqAssetDenom, unit, err := types.GetDenomFromStr(args[2])
			if err != nil {
				return err
			}
			if unit != types.Unit_STANDARD {
				return errors.New("Denom must be in standard/whole unit (e.g. sei instead of usei)")
			}

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := types.NewMsgRegisterPair(
				clientCtx.GetFromAddress().String(),
				argContractAddr,
				reqPriceDenom,
				reqAssetDenom,
			)
			if err := msg.ValidateBasic(); err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
