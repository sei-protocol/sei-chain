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

// The node kind is the one of those two a reader has to account for.
//
// A generated file states it at the top, so a caller that hands a decoded file to the resolver finds no
// section declaring it and files it beside an operator's typos. Declaring it is not the answer: the kind
// of node is what the resolution is asked about, so a declared answer for it would be the question. The
// reader is where this is settled, by exempting this one key from what it reports as unknown, and saying
// so here is what stops that being rediscovered.

// removedFromTheNode are the root paths this section does not declare because nothing reads them.
//
// The node marks each field deprecated and nothing in the tree reads any of them: out-of-process ABCI
// was removed, and so was peer filtering through it. The generated file writes twelve root keys and none
// of these three. Two of them state an affirmative value in the node's own defaults, an address and a
// transport name, so declaring them put settings for a transport the binary no longer has into the key
// space a writer would render from.
var removedFromTheNode = []string{"proxy-app", "abci", "filter-peers"}

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
// The node marks them two ways. Most carry the prefix on the field name; the leader election one carries
// the standard comment instead, which is why it went on being declared as a settable key. Its declared
// value was the affirmative one, so an operator reading a generated file would find the behaviour named
// and switchable and neither is true.
//
// The reader has a check that names the removed settings an operator wrote, and it reaches half of them.
// Most of the rest are held on a value rather than a pointer, so a written zero cannot be told from an
// unwritten field, though a written value that is not the zero can be. One is a pointer the check could
// name as cheaply as the seven pointers it already names, and simply does not. Which ones those are, and
// which of the two reasons each has, is recorded beside the check that measures it rather than counted
// twice here. Nothing calls the check in any case, so leaving these out of the file is what an operator
// actually gets.
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

// neverReachTheMempool are the mempool paths this section does not declare.
//
// No code reads them. That is the criterion, and it is a stronger one than the marking on the fields,
// where two of the three are marked dead at their destination and the third carries only a note about an
// upstream issue. Declaring any of them would offer a key that changes nothing about how the node runs.
//
// Two paths carry a written mempool value into the running node. The conversion into the mempool's own
// configuration is one, and a test drives it. The transaction reactor is the other, reading several
// settings straight off this struct without that conversion, and it sits behind an internal package that
// this one cannot import, so that half is established by reading it rather than by a test. Naming only
// the conversion would also have made the rule narrower than the list: three settings it does not carry
// are declared, and correctly, because the reactor reads them.
//
// The pair named almost the same as two of these is live and does reach the mempool. That near-collision
// is why this is measured rather than reasoned about: a transaction lifetime is a setting and the pending
// lifetime beside it is not.
var neverReachTheMempool = []string{
	"max-batch-bytes",
	"pending-ttl-duration",
	"pending-ttl-num-blocks",
}

// fixedForEveryNode is the metric prefix this section does not declare.
//
// The node marks the field deprecated and states that its metrics always use one fixed prefix, so the
// value is not the node's to vary and not an operator's to set. Its declared value was that fixed prefix,
// which reads as a setting whose default happens to be this, and the generated file does not write it.
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
	declareSection(P2PSectionName, &tmcfg.P2PConfig{}, p2pDefaults,
		filledFromTheCommandLine, derivedFromTheConnectionLimit, readByNothing)
	declareSection(RPCSectionName, &tmcfg.RPCConfig{}, rpcDefaults,
		filledFromTheCommandLine)
	declareSection(ConsensusSectionName, &tmcfg.ConsensusConfig{}, consensusDefaults,
		append([]string{filledFromTheCommandLine}, removedSettings...)...)
	declareSection(MempoolSectionName, &tmcfg.MempoolConfig{}, mempoolDefaults,
		append([]string{filledFromTheCommandLine}, neverReachTheMempool...)...)
	declareSection(StateSyncSectionName, &tmcfg.StateSyncConfig{}, stateSyncDefaults)
	declareSection(TxIndexSectionName, &tmcfg.TxIndexConfig{}, txIndexDefaults)
	declareSection(InstrumentationSectionName, &tmcfg.InstrumentationConfig{},
		instrumentationDefaults, fixedForEveryNode)
	declareSection(PrivValidatorSectionName, &tmcfg.PrivValidatorConfig{},
		privValidatorDefaults, filledFromTheCommandLine)
	declareSection(SelfRemediationSectionName, &tmcfg.SelfRemediationConfig{},
		selfRemediationDefaults)
	declareRootKeys(RootSectionName, &nodeRootSchema{}, rootDefaults,
		append(append([]string{}, notWritableInThisFile...), removedFromTheNode...)...)
}

// registeredHere are the sections this package put in the registry, recorded as each one is registered.
//
// A test needs to know which sections are this package's, and a list written beside the registrations is a
// second statement of the same fact: a section registered and left off the list is one nothing here checks,
// which is the case the list exists to prevent. Recorded by the registration itself instead, so the two
// cannot disagree.
var registeredHere []string

// declareSection registers a section and records that it belongs to this package.
func declareSection(name string, prototype any, defaults func(registry.Mode) any, excluding ...string) {
	registry.RegisterSectionExcluding(name, prototype, defaults, excluding...)
	registeredHere = append(registeredHere, name)
}

// declareRootKeys registers a section whose keys sit at the root of the file, and records it the same way.
func declareRootKeys(name string, prototype any, defaults func(registry.Mode) any, excluding ...string) {
	registry.RegisterRootKeysExcluding(name, prototype, defaults, excluding...)
	registeredHere = append(registeredHere, name)
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

// filledFromTheCommandLine is the path six of these sections carry and none declares.
//
// Each holds a root directory field tagged the same as the one at the top of the file, and the node fills
// every one of them from the command line after the file is read. Five leave it out under this name and
// the section at the top of the file leaves it out under its own, which is why the count here is not the
// number of times this constant appears. So the file never carries the value, and
// what these sections state for it is the empty string. Declaring it would hand a delivery an empty root to
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
// carries. The peer seeds come from the chain identifier and the node name is a required argument to the
// command, both runtime inputs and neither a node kind, so no answer keyed on mode can be right. The node
// name is the sharper of the two: its declared value is the hostname of whatever machine is asking, so
// this key resolves differently on every host. A writer takes both from the generator instead.
var filledByTheGenerator = []string{P2PSectionName + ".bootstrap-peers", "moniker"}

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
// The same values for every mode. How long a node waits at each step of a round has to agree across the
// validator set for the set to reach a decision, so a value that followed from the kind of node asking
// would be this package proposing that they disagree.
func consensusDefaults(mode registry.Mode) any { return *forMode(mode).Consensus }

// mempoolDefaults is what a generated file carries for the mempool section.
//
// The same values for every mode. What a node holds before a transaction is decided is a limit on its own
// memory and bandwidth, and nothing in the binary makes one follow from what kind of node is asking.
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
