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

// removedSettings are the consensus paths this section does not declare. Each names a field the node's
// struct marks deprecated, so the key would offer a setting that a written value cannot change.
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
	"stateless-leader-election",
}

// neverReachTheMempool are the mempool paths this section does not declare. No code reads them, so
// declaring one would offer a setting that changes nothing, even though a generated file writes all
// three. The similarly spelled ttl-duration and ttl-num-blocks are live and stay declared.
var neverReachTheMempool = []string{
	unreadAndUnmarked,
	"pending-ttl-duration",
	"pending-ttl-num-blocks",
}

// unreadAndUnmarked is the one member of neverReachTheMempool whose field carries no deprecation note; the
// field points at an upstream issue instead. No check on markings reaches it, so the name states the
// reason for leaving it out.
const unreadAndUnmarked = "max-batch-bytes"

// reachesNoReactor is the self remediation path this section does not declare.
//
// The other four settings in that group reach a reactor, three to the block sync one and one to the state
// sync one. This one reaches neither: the node checks its bound when validating and nothing else reads it,
// so a written value changes nothing about when the node restarts. Its field carries no deprecation note
// either, so no check on markings accounts for it.
const reachesNoReactor = "p2p-no-peers-available-window-seconds"

// fixedForEveryNode is the metric prefix this section does not declare. The node marks the field
// deprecated and states that its metrics always use one fixed prefix, so the value is not an operator's
// to set.
const fixedForEveryNode = "namespace"

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
		filledFromTheCommandLine, derivedFromTheConnectionLimit, readByNothing)
	registry.RegisterSectionExcluding(RPCSectionName, &tmcfg.RPCConfig{}, rpcDefaults,
		filledFromTheCommandLine)
	registry.RegisterSectionExcluding(ConsensusSectionName, &tmcfg.ConsensusConfig{}, consensusDefaults,
		append([]string{filledFromTheCommandLine}, removedSettings...)...)
	registry.RegisterSectionExcluding(MempoolSectionName, &tmcfg.MempoolConfig{}, mempoolDefaults,
		append([]string{filledFromTheCommandLine}, neverReachTheMempool...)...)
	registry.RegisterSection(StateSyncSectionName, &tmcfg.StateSyncConfig{}, stateSyncDefaults)
	registry.RegisterSection(TxIndexSectionName, &tmcfg.TxIndexConfig{}, txIndexDefaults)
	registry.RegisterSectionExcluding(InstrumentationSectionName, &tmcfg.InstrumentationConfig{},
		instrumentationDefaults, fixedForEveryNode)
	registry.RegisterSectionExcluding(PrivValidatorSectionName, &tmcfg.PrivValidatorConfig{},
		privValidatorDefaults, filledFromTheCommandLine)
	registry.RegisterSectionExcluding(SelfRemediationSectionName, &tmcfg.SelfRemediationConfig{},
		selfRemediationDefaults, reachesNoReactor)
}

// forMode is the configuration the seid init command writes for a kind of node.
//
// Pinned to that command's own pipeline rather than restated here: the defaults the node's package
// declares, then the mode rules the binary applies to them. So a declared value is what a generated file
// carries for every key but the ones in filledByTheGenerator, and a change to either half moves this
// with it. That command goes on to set those from inputs no mode carries, which is why they are named
// rather than described.
//
// The mode is written onto the configuration before the rules run, because the rules read it from there
// rather than taking it as an argument. The command reaches the same rules by a different route for an
// archive node, writing the full-node mode before it runs them, and the two agree only because the rules
// answer alike for both. A rule that stopped answering alike would leave this the more specific of the
// two answers, so the test holding what varies by node kind is what keeps them together.
func forMode(mode registry.Mode) *tmcfg.Config {
	out := tmcfg.DefaultConfig()
	out.Mode = string(mode)
	params.SetTendermintConfigByMode(out)
	return out
}

// filledFromTheCommandLine is the root directory path several of these sections carry and none declares.
//
// Each holds a field tagged the same as the one at the top of the file, and the node fills every one of
// them from the command line after the file is read, so the file never carries the value and what these
// sections would state for it is the empty string. Declaring it would hand a delivery an empty root to
// write over a running node's, and a node that cannot find its data directory, its genesis file or its
// signing key does not start.
const filledFromTheCommandLine = "home"

// derivedFromTheConnectionLimit is the ceiling this section leaves out because unset is the setting.
//
// The field is a pointer the defaults leave unset, and unset is what selects the behaviour: the node
// derives a ceiling from the total connection limit instead. Declaring it would need a default, and any
// number written here would be this package inventing one that no generated file carries.
const derivedFromTheConnectionLimit = "max-outbound-connections"

// readByNothing is the dial hook this section leaves out because no code reads it.
//
// The field is declared beside the peer-to-peer settings and its own comment calls it a testing parameter,
// but nothing in the tree reads it and the generated file does not write it. Declaring it would offer a
// key that changes nothing about how the node runs.
const readByNothing = "test-dial-fail"

// filledByTheGenerator are keys the init command sets after the pipeline forMode mirrors, from an input a
// mode does not carry.
//
// Declared rather than excluded, because the generated file writes each one into every node's
// configuration, and a key this space refuses is a key an operator's own file reports as unknown. What
// they cost is the invariant on forMode: for these, a declared value is not what a generated file
// carries. The peer seeds come from the chain identifier, which is a runtime input and not a node kind,
// so no answer keyed on mode can be right. A writer takes the value from the generator instead.
var filledByTheGenerator = []string{P2PSectionName + ".bootstrap-peers"}

// p2pDefaults is what a generated file carries for the peer-to-peer section.
//
// Answered per mode. Three of these settings follow from what kind of node is asking: a validator refuses
// duplicate addresses, a seed accepts them and raises its connection ceiling because serving peers is what
// it exists for, and a node that serves queries binds an address where a validator leaves the default.
//
// The advertised address and the peer list are declared even though a cluster patches them in from live
// discovery after the file is written. A node run by hand has nothing patching them, and an operator
// sets both, so refusing the keys would leave that node unable to state its own address or its own peers
// through this key space while its configuration file accepts both. What a patch and a declaration do to
// each other is a question for whatever writes the file, and the same question the peer seeds raise: it
// is answered by what a writer preserves, not by which keys exist.
func p2pDefaults(mode registry.Mode) any { return *forMode(mode).P2P }

// rpcDefaults is what a generated file carries for the remote procedure call section.
//
// Answered per mode, for the listen address alone: a node that serves queries binds one and a validator
// does not.
func rpcDefaults(mode registry.Mode) any { return *forMode(mode).RPC }

// consensusDefaults is what a generated file carries for the consensus section.
//
// The same values for every mode: nothing in the binary derives one of these from a node kind. What this
// section declares is a log path, a proposer's own empty-block policy, a gossip mode, two reactor sleeps
// and a local restart check. The timings that do have to agree across the validator set are the paths
// removedSettings leaves out.
func consensusDefaults(mode registry.Mode) any { return *forMode(mode).Consensus }

// mempoolDefaults is what a generated file carries for the mempool section.
//
// The same values for every mode: these limits bound one node's own memory and bandwidth, and nothing in
// the binary derives one from a node kind.
func mempoolDefaults(mode registry.Mode) any { return *forMode(mode).Mempool }

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
