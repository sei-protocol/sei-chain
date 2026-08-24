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

	// RootSectionName identifies the keys that sit at the top of the file with no table of their own. The
	// name is for lookups and reports and is not part of any key.
	RootSectionName = "node_base"
)

// notWritableInThisFile are root paths this section does not declare.
//
// Neither is a setting an operator can usefully write here. The home directory is where this file is found,
// so a value inside it would be the file naming its own location, and the command line already carries it.
// The node mode is the same fact the file states at the top under its own name, and declaring a second
// spelling would let the two disagree, with the resolution answering for one and the node reading the
// other.
var notWritableInThisFile = []string{"home", "mode"}

// nodeRootSchema declares the keys that sit at the root of the node's configuration file.
//
// The node's own top-level type carries these and the nine tables both, so declaring against it directly
// would declare every table's keys a second time. This squashes the same base group that type squashes, so
// fourteen spellings still come from the node's own tags, and restates only the two fields it holds beside
// that group. A test holds those two against it.
type nodeRootSchema struct {
	tmcfg.BaseConfig `mapstructure:",squash"`

	AutobahnConfigFile      string `mapstructure:"autobahn-config-file"`
	HashVaultDisabledUnsafe bool   `mapstructure:"hash-vault-disabled-unsafe"`
}

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
		notDeclaredBy(P2PSectionName, filledFromTheCommandLine, "max-outbound-connections")...)
	registry.RegisterSectionExcluding(RPCSectionName, &tmcfg.RPCConfig{}, rpcDefaults,
		filledFromTheCommandLine)
	registry.RegisterSectionExcluding(ConsensusSectionName, &tmcfg.ConsensusConfig{}, consensusDefaults,
		append([]string{filledFromTheCommandLine}, removedSettings...)...)
	registry.RegisterSectionExcluding(MempoolSectionName, &tmcfg.MempoolConfig{}, mempoolDefaults,
		filledFromTheCommandLine)
	registry.RegisterSectionExcluding(StateSyncSectionName, &tmcfg.StateSyncConfig{}, stateSyncDefaults,
		notDeclaredBy(StateSyncSectionName, "rpc-servers")...)
	registry.RegisterSection(TxIndexSectionName, &tmcfg.TxIndexConfig{}, txIndexDefaults)
	registry.RegisterSection(InstrumentationSectionName, &tmcfg.InstrumentationConfig{},
		instrumentationDefaults)
	registry.RegisterSectionExcluding(PrivValidatorSectionName, &tmcfg.PrivValidatorConfig{},
		privValidatorDefaults, filledFromTheCommandLine)
	registry.RegisterSection(SelfRemediationSectionName, &tmcfg.SelfRemediationConfig{},
		selfRemediationDefaults)
	registry.RegisterRootKeysExcluding(RootSectionName, &nodeRootSchema{}, rootDefaults,
		notDeclaredBy(RootSectionName, append(notWritableInThisFile, noLongerHasAnyEffect...)...)...)

	// Each of these reaches its reader by a decode rather than a lookup, so the boot delivers them a
	// second way. Declared in the same loop that names them, so a section registered above and forgotten
	// here would be a section this package does not list at all.
	for _, name := range declaredSectionNames() {
		registry.DeclareDecodedNotLookedUp(name,
			"decoded into the node's own configuration struct by the boot's handler, which reads that "+
				"file once; nothing looks these keys up afterwards")
	}
}

// declaredSectionNames are the sections this package registers.
//
// One list, read by the registration that marks them all as decoded and by the test that holds the two
// against each other, so a section can not be registered without being delivered.
func declaredSectionNames() []string {
	return []string{
		P2PSectionName, RPCSectionName, ConsensusSectionName, MempoolSectionName, StateSyncSectionName,
		TxIndexSectionName, InstrumentationSectionName, PrivValidatorSectionName,
		SelfRemediationSectionName, RootSectionName,
	}
}

// notDeclaredBy gathers every path one section leaves out, from the reasons that apply to it.
//
// Gathered rather than written out per registration, because a path left out for a reason that covers
// several sections is easy to add to one and forget in the others.
func notDeclaredBy(section string, also ...string) []string {
	out := append([]string{}, also...)
	out = append(out, writtenBySomethingOutsideTheBinary[section]...)
	out = append(out, forTestsOnly[section]...)
	return out
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

// writtenBySomethingOutsideTheBinary are paths a node's own file receives from elsewhere at boot.
//
// The rule that keeps these out is not that the binary fills them in, which is what the root directory
// does. It is that something else does, and this file cannot see it. The cluster's node controller resolves
// a peer set from live discovery and patches the addresses in; a node computes a trust height and hash from
// the chain tip each time it starts; a moniker is stamped per instance. A value declared here would be
// decoded over whichever of those already ran, and the file it came from would keep saying otherwise.
//
// The moniker has a second reason on its own. Its default is the host name of whatever machine resolved it,
// so no two machines agree on what this key declares.
var writtenBySomethingOutsideTheBinary = map[string][]string{
	P2PSectionName:       {"external-address", "persistent-peers"},
	StateSyncSectionName: {"trust-height", "trust-hash"},
	RootSectionName:      {"moniker"},
}

// noLongerHasAnyEffect are paths a reader keeps and ignores.
//
// The out-of-process application interface was removed, and the flag that carries this key is marked
// deprecated where it is declared, saying the flag is ignored. A declared key whose only effect is nothing
// is a setting an operator can spend an afternoon on.
var noLongerHasAnyEffect = []string{"abci"}

// forTestsOnly are paths that exist to make a node misbehave.
//
// One makes every dial fail and one runs the node against a stub application. Neither has a use on a real
// network, and both are reachable from a file an operator edits by hand, on a node whose request surface
// faces the outside.
var forTestsOnly = map[string][]string{
	P2PSectionName:  {"test-dial-fail"},
	RootSectionName: {"mock-app"},
}

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

// rootDefaults is what a generated file carries at the top of the node's configuration file.
//
// The same values for every mode. These name where a node keeps its data and how it logs, and nothing in
// the binary makes either follow from what kind of node is asking.
func rootDefaults(mode registry.Mode) any {
	live := forMode(mode)
	return nodeRootSchema{
		BaseConfig:              live.BaseConfig,
		AutobahnConfigFile:      live.AutobahnConfigFile,
		HashVaultDisabledUnsafe: live.HashVaultDisabledUnsafe,
	}
}

// selfRemediationDefaults is what a generated file carries for the self remediation section.
//
// The same values for every mode. These are the thresholds at which a node restarts itself, and each one
// describes a node that has stopped making progress, which is the same condition whatever the node is for.
func selfRemediationDefaults(mode registry.Mode) any { return *forMode(mode).SelfRemediation }
