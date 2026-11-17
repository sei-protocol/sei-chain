// Package cli serves seid's gov, upgrade, params and block commands for an
// EVM-only (giga) chain over its EVM JSON-RPC, keeping the V2 command names,
// arguments and output.
package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
)

const (
	// FlagEVMRPC is the EVM JSON-RPC endpoint of an EVM-only (giga) chain.
	FlagEVMRPC = "evm-rpc"
	// FlagGasFeeCap is the gas price, in wei, of a transaction sent with
	// --evm-rpc.
	FlagGasFeeCap = "gas-fee-cap"
	// FlagName is the plan cancel-software-upgrade cancels.
	FlagName = "name"
)

// handler runs a command against the precompiles.
type handler func(cmd *cobra.Command, args []string) error

var errBothEndpoints = fmt.Errorf("--%s and --%s cannot be used together", FlagEVMRPC, flags.FlagNode)

// routedTrees are the commands, and the trees of commands, Route adds --evm-rpc
// to.
var routedTrees = [][]string{
	{"tx", "gov"},
	{"query", "gov"},
	{"query", "upgrade"},
	{"query", "params"},
	{"query", "block"},
}

// handlers are the commands served with --evm-rpc, by path below the root.
// Every other command in a routed tree fails with --evm-rpc.
var handlers = map[string]handler{
	"tx gov submit-proposal":                         submitTextProposal,
	"tx gov submit-proposal software-upgrade":        submitSoftwareUpgrade,
	"tx gov submit-proposal cancel-software-upgrade": submitCancelSoftwareUpgrade,
	"tx gov submit-proposal param-change":            submitParamChange,
	"tx gov deposit":                                 deposit,
	"tx gov vote":                                    vote,
	"tx gov weighted-vote":                           weightedVote,

	"query gov proposal":  queryProposal,
	"query gov proposals": queryProposals,
	"query gov vote":      queryVote,
	"query gov votes":     queryVotes,
	"query gov deposit":   queryDeposit,
	"query gov deposits":  queryDeposits,
	"query gov tally":     queryTally,
	"query gov params":    queryGovParams,
	"query gov param":     queryGovParam,

	"query upgrade plan":    queryPlan,
	"query upgrade applied": queryApplied,

	"query params subspace": querySubspace,

	"query block": queryBlock,
}

// Route adds --evm-rpc to seid's gov, upgrade, params and block commands. A
// command given --evm-rpc runs against that EVM-only chain.
func Route(root *cobra.Command) {
	for _, path := range routedTrees {
		tree, _, err := root.Find(path)
		if err != nil || tree.CommandPath() != root.Name()+" "+strings.Join(path, " ") {
			continue
		}
		tree.PersistentFlags().String(FlagEVMRPC, "", "EVM JSON-RPC endpoint of an EVM-only (giga) chain, used instead of --node")
		if path[0] == "tx" {
			tree.PersistentFlags().Uint64(FlagGasFeeCap, 0, "Gas price in wei with --evm-rpc; 0 uses the node's suggested price")
		}
		wrapTree(tree)
	}
	if cancel, _, err := root.Find([]string{"tx", "gov", "submit-proposal", "cancel-software-upgrade"}); err == nil && cancel.Name() == "cancel-software-upgrade" {
		cancel.Flags().String(FlagName, "", "Name of the plan to cancel, with --evm-rpc; defaults to the scheduled plan after confirmation")
	}
}

// wrapTree wraps every runnable command at or below c.
func wrapTree(c *cobra.Command) {
	for _, sub := range c.Commands() {
		wrapTree(sub)
	}
	if c.RunE == nil && c.Run == nil {
		return
	}
	v2 := c.RunE
	if v2 == nil {
		run := c.Run
		v2 = func(cmd *cobra.Command, args []string) error {
			run(cmd, args)
			return nil
		}
		c.Run = nil
	}
	evm := handlers[commandKey(c)]
	c.RunE = func(cmd *cobra.Command, args []string) error {
		return dispatch(cmd, args, v2, evm)
	}
}

// dispatch runs evm when --evm-rpc is set and v2 otherwise.
func dispatch(cmd *cobra.Command, args []string, v2, evm handler) error {
	if !flagChanged(cmd, FlagEVMRPC) {
		if flagChanged(cmd, FlagName) {
			return fmt.Errorf("--%s needs --%s", FlagName, FlagEVMRPC)
		}
		return v2(cmd, args)
	}
	if flagChanged(cmd, flags.FlagNode) {
		return errBothEndpoints
	}
	if evm == nil {
		return fmt.Errorf("%q is not supported with --%s", commandKey(cmd), FlagEVMRPC)
	}
	return evm(cmd, args)
}

// commandKey is c's path below the root, e.g. "tx gov vote".
func commandKey(c *cobra.Command) string {
	return strings.TrimPrefix(c.CommandPath(), c.Root().Name()+" ")
}

func flagChanged(cmd *cobra.Command, name string) bool {
	f := cmd.Flags().Lookup(name)
	return f != nil && f.Changed
}

var errNoEndpoint = errors.New("--" + FlagEVMRPC + " is empty")
