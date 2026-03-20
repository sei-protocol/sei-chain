package debug

import (
	"github.com/spf13/cobra"
)

const (
	flagNodeRPCAddr = "rpc-laddr"
	flagProfAddr    = "pprof-laddr"
	flagFrequency   = "frequency"
)

func GetDebugCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug",
		Short: "A utility to kill or watch a Tendermint process while aggregating debugging data",
	}
	cmd.PersistentFlags().SortFlags = true
	cmd.PersistentFlags().String(
		flagNodeRPCAddr,
		"tcp://localhost:26657",
		"the Tendermint node's RPC address <host>:<port>)",
	)

	cmd.AddCommand(getKillCmd())
	cmd.AddCommand(getDumpCmd())
	return cmd

}
