package tx

import (
	"strconv"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
)

var _ = strconv.Itoa(0)

func CmdRegisterContract() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-contract [contract address] [code id] [need hook] [need order matching] [dependency1,dependency2,...]",
		Short: "Register exchange contract",
		Args:  cobra.MinimumNArgs(4),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			argContractAddr := args[0]
			argCodeID, err := cast.ToUint64E(args[1])
			if err != nil {
				return err
			}
			argNeedHook := args[2] == "true"
			argNeedMatching := args[3] == "true"
			dependencies := []*types.ContractDependencyInfo{}
			for _, dependency := range args[4:] {
				dependencies = append(dependencies, &types.ContractDependencyInfo{Dependency: dependency})
			}

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := types.NewMsgRegisterContract(
				clientCtx.GetFromAddress().String(),
				argCodeID,
				argContractAddr,
				argNeedHook,
				argNeedMatching,
				dependencies,
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
