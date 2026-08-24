package tendermintbase

import (
	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/config/registry"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// The names these sections have in the configuration key space.
const (
	P2PSectionName       = "p2p"
	RPCSectionName       = "rpc"
	ConsensusSectionName = "consensus"
	MempoolSectionName   = "mempool"
)

// removedSettings are the consensus paths this section does not declare.
//
// Every one is a setting the node removed, and the struct marks each field deprecated. The fields are kept
// so a decode can tell that an operator set one, and declaring any of them would offer a key that changes
// nothing about how the node runs.
//
// The reader has a check that names the removed settings an operator wrote, and it reaches eight of these
// fifteen. Six are durations or booleans, where a written zero and an unwritten field are the same value,
// so no check can tell them apart. One more the check simply omits. Nothing calls the check in any case, so
// leaving these out of the file is what an operator actually gets.
var removedSettings = []string{
	"unsafe-overrides-enabled",
	"unsafe-propose-timeout-override",
	"unsafe-propose-timeout-delta-override",
	"unsafe-vote-timeout-override",
	"unsafe-vote-timeout-delta-override",
	"unsafe-commit-timeout-override",
	"unsafe-bypass-commit-timeout-override",
	"timeout-propose",
	"timeout-propose-delta",
	"timeout-prevote",
	"timeout-prevote-delta",
	"timeout-precommit",
	"timeout-precommit-delta",
	"timeout-commit",
	"skip-timeout-commit",
}

// Registration puts these sections in the configuration registry.
//
// Neither the package that defines these settings nor the package that decides them can register them. The
// struct they are read into belongs to the node's own configuration package, and the rules that vary them
// by node kind live in the parameters package, which imports that struct. So the importing direction is
// already fixed and only a third package can see both.
//
// The keys derive from the struct's mapstructure tags, which is what the node's reader decodes through, so
// a key here is a key that reader resolves rather than a second spelling of it.
func init() {
	registry.RegisterSectionExcluding(P2PSectionName, &tmcfg.P2PConfig{}, p2pDefaults,
		filledFromTheCommandLine, "max-outbound-connections")
	registry.RegisterSectionExcluding(RPCSectionName, &tmcfg.RPCConfig{}, rpcDefaults,
		filledFromTheCommandLine)
	registry.RegisterSectionExcluding(ConsensusSectionName, &tmcfg.ConsensusConfig{}, consensusDefaults,
		append([]string{filledFromTheCommandLine}, removedSettings...)...)
	registry.RegisterSectionExcluding(MempoolSectionName, &tmcfg.MempoolConfig{}, mempoolDefaults,
		filledFromTheCommandLine)
}

// forMode is the configuration the seid init command writes for a kind of node.
//
// Pinned to that command's own pipeline rather than restated here: the defaults the node's package
// declares, then the mode rules the binary applies to them. A declared value is therefore what a
// generated file carries, and a change to either half moves this with it.
//
// The mode is written onto the configuration before the rules run, because the rules read it from there
// rather than taking it as an argument.
func forMode(mode registry.Mode) *tmcfg.Config {
	out := tmcfg.DefaultConfig()
	out.Mode = string(mode)
	params.SetTendermintConfigByMode(out)
	return out
}

// filledFromTheCommandLine is the path five of these sections carry and none declares.
//
// Each holds a root directory field tagged the same as the one at the top of the file, and the node fills
// every one of them from the command line after the file is read. So the file never carries the value, and
// what these sections state for it is the empty string. Declaring it would hand a delivery an empty root to
// write over a running node's, and a node that cannot find its data directory, its genesis file or its
// signing key does not start.
const filledFromTheCommandLine = "home"

// The other path the peer-to-peer section does not declare.
//
// The outbound connection ceiling is a pointer the defaults leave unset, and unset is what selects the
// behaviour: the node derives a ceiling from the total connection limit instead. Declaring it would need a
// default, and any number written here would be this package inventing one that no generated file carries.

// p2pDefaults is what a generated file carries for the peer-to-peer section.
//
// Answered per mode. Three of these settings follow from what kind of node is asking: a validator refuses
// duplicate addresses, a seed accepts them and raises its connection ceiling because serving peers is what
// it exists for, and a node that serves queries binds an address where a validator leaves the default.
func p2pDefaults(mode registry.Mode) any { return *forMode(mode).P2P }

// rpcDefaults is what a generated file carries for the remote procedure call section.
//
// Answered per mode, for the listen address alone: a node that serves queries binds one and a validator
// does not.
func rpcDefaults(mode registry.Mode) any { return *forMode(mode).RPC }

// consensusDefaults is what a generated file carries for the consensus section.
//
// The same values for every mode. How long a node waits at each step of a round has to agree across the
// validator set for the set to reach a decision, so a value that followed from the kind of node asking
// would be this package proposing that they disagree.
func consensusDefaults(mode registry.Mode) any { return *forMode(mode).Consensus }

// mempoolDefaults is what a generated file carries for the mempool section.
//
// The same values for every mode. What a node holds before a transaction is decided is a limit on its own
// memory and bandwidth, and nothing in the binary makes one follow from what kind of node is asking.
func mempoolDefaults(mode registry.Mode) any { return *forMode(mode).Mempool }
