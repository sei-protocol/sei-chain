package tx

import (
	"strconv"
	"strings"

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
		Use:   "register-contract [contract address] [code id] [(deprecated)] [need order matching] [deposit] [dependency1,dependency2,...]",
		Short: "Register exchange contract",
		Long: strings.TrimSpace(`
			Register a contract with the dex module for order matching hooks. The available order matching functions are BulkOrderPlacements, BulkOrderCancellations, Settlement. A deposit can also be specified as the initial rent to allocate for the execution of the order matching.
			Other orderbooks that are dependencies can also be specified so that dex orderbook processing can be performed in the appropriate order.
		`),
		Args: cobra.MinimumNArgs(5),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			argContractAddr := args[0]
			argCodeID, err := cast.ToUint64E(args[1])
			if err != nil {
				return err
			}
			argNeedMatching, err := strconv.ParseBool(args[3])
			if err != nil {
				return err
			}
			argDeposit, err := cast.ToUint64E(args[4])
			if err != nil {
				return err
			}
			var dependencies []*types.ContractDependencyInfo
			for _, dependency := range args[5:] {
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
				argNeedMatching,
				dependencies,
				argDeposit,
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
