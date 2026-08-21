package cmd

import (
	"github.com/spf13/cobra"
)

// StakingCmd returns offline staking maintenance commands.
func StakingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "staking",
		Short: "Offline staking maintenance tools",
	}
	cmd.AddCommand(BackfillDelegationIndexCmd())
	return cmd
}
