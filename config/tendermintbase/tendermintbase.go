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

	StateSyncSectionName       = "statesync"
	TxIndexSectionName         = "tx-index"
	InstrumentationSectionName = "instrumentation"
	PrivValidatorSectionName   = "priv-validator"
	SelfRemediationSectionName = "self-remediation"
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
		"max-outbound-connections")
	registry.RegisterSection(RPCSectionName, &tmcfg.RPCConfig{}, rpcDefaults)
	registry.RegisterSectionExcluding(ConsensusSectionName, &tmcfg.ConsensusConfig{}, consensusDefaults,
		removedSettings...)
	registry.RegisterSection(MempoolSectionName, &tmcfg.MempoolConfig{}, mempoolDefaults)
	registry.RegisterSectionExcluding(StateSyncSectionName, &tmcfg.StateSyncConfig{}, stateSyncDefaults,
		"rpc-servers")
	registry.RegisterSection(TxIndexSectionName, &tmcfg.TxIndexConfig{}, txIndexDefaults)
	registry.RegisterSection(InstrumentationSectionName, &tmcfg.InstrumentationConfig{},
		instrumentationDefaults)
	registry.RegisterSection(PrivValidatorSectionName, &tmcfg.PrivValidatorConfig{}, privValidatorDefaults)
	registry.RegisterSection(SelfRemediationSectionName, &tmcfg.SelfRemediationConfig{},
		selfRemediationDefaults)
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

// The one path this section does not declare.
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

// The one path the state sync section does not declare.
//
// The list of servers to fetch a snapshot from has no default and cannot have one: the addresses are the
// operator's own peers. An empty list is not a value they can inherit, and any address written here would
// name a host this binary does not know exists.

// stateSyncDefaults is what a generated file carries for the state sync section.
//
// The same values for every mode. Whether a node starts from a snapshot is a decision about how it is being
// brought up rather than about what it will be, and every kind of node can be brought up either way.
func stateSyncDefaults(mode registry.Mode) any { return *forMode(mode).StateSync }

// txIndexDefaults is what a generated file carries for the transaction index section.
//
// Answered per mode, for the indexer alone. A node that serves queries indexes transactions so it can
// answer them, and a validator and a seed serve none, so they index nothing and keep the write.
func txIndexDefaults(mode registry.Mode) any { return *forMode(mode).TxIndex }

// instrumentationDefaults is what a generated file carries for the instrumentation section.
//
// The same values for every mode. What a node measures about itself is a decision about how it is operated,
// and an operator who collects metrics collects them from every kind of node they run.
func instrumentationDefaults(mode registry.Mode) any { return *forMode(mode).Instrumentation }

// privValidatorDefaults is what a generated file carries for the signing key section.
//
// The same values for every mode. These are paths and an address for reaching a signer, and a node that
// does not sign simply does not use them, so varying them by kind would state a difference the binary does
// not make.
func privValidatorDefaults(mode registry.Mode) any { return *forMode(mode).PrivValidator }

// selfRemediationDefaults is what a generated file carries for the self remediation section.
//
// The same values for every mode. These are the thresholds at which a node restarts itself, and each one
// describes a node that has stopped making progress, which is the same condition whatever the node is for.
func selfRemediationDefaults(mode registry.Mode) any { return *forMode(mode).SelfRemediation }
