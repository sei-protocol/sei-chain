package cli

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/sei-protocol/sei-chain/x/seinet/types"
	"github.com/spf13/cobra"
)

// CmdUnlockHardwareKey creates a command to unlock hardware key authorization.
func CmdUnlockHardwareKey() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlock-hardware-key",
		Short: "Authorize covenant commits with your hardware key",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := &types.MsgUnlockHardwareKey{Creator: clientCtx.GetFromAddress().String()}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
