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
	// DefaultEVMRPC is the --evm-rpc endpoint when the flag is not given.
	DefaultEVMRPC = "http://localhost:8545"
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

// handlers are the commands served over the EVM JSON-RPC, by path below the
// root. Every other runnable command in a routed tree needs --node.
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

// Route serves seid's gov, upgrade, params and block commands over the EVM
// JSON-RPC at --evm-rpc. A command given --node runs against the Tendermint RPC
// instead.
func Route(root *cobra.Command) {
	for _, path := range routedTrees {
		tree, _, err := root.Find(path)
		if err != nil || tree.CommandPath() != root.Name()+" "+strings.Join(path, " ") {
			continue
		}
		tree.PersistentFlags().String(FlagEVMRPC, DefaultEVMRPC, "EVM JSON-RPC endpoint of an EVM-only (giga) chain; --node uses the Tendermint RPC instead")
		if path[0] == "tx" {
			tree.PersistentFlags().Uint64(FlagGasFeeCap, 0, "Gas price in wei of an EVM transaction; 0 uses the node's suggested price")
		}
		wrapTree(tree)
	}
	if cancel, _, err := root.Find([]string{"tx", "gov", "submit-proposal", "cancel-software-upgrade"}); err == nil && cancel.Name() == "cancel-software-upgrade" {
		cancel.Flags().String(FlagName, "", "Name of the plan to cancel; defaults to the scheduled plan after confirmation")
	}
}

// wrapTree wraps every runnable command at or below c, except command groups
// with no EVM handler, which keep printing their help.
func wrapTree(c *cobra.Command) {
	for _, sub := range c.Commands() {
		wrapTree(sub)
	}
	evm := handlers[commandKey(c)]
	if (c.RunE == nil && c.Run == nil) || (evm == nil && c.HasSubCommands()) {
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
	c.RunE = func(cmd *cobra.Command, args []string) error {
		return dispatch(cmd, args, v2, evm)
	}
}

// dispatch runs v2 when --node is set and evm otherwise.
func dispatch(cmd *cobra.Command, args []string, v2, evm handler) error {
	if flagChanged(cmd, flags.FlagNode) {
		if flagChanged(cmd, FlagEVMRPC) {
			return errBothEndpoints
		}
		if flagChanged(cmd, FlagName) {
			return fmt.Errorf("--%s cannot be used with --%s", FlagName, flags.FlagNode)
		}
		return v2(cmd, args)
	}
	if evm == nil {
		return fmt.Errorf("%q is not supported on an EVM-only chain; use --%s for a chain with a Tendermint RPC", commandKey(cmd), flags.FlagNode)
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
