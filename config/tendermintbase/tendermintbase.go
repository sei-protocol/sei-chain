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

	// RootSectionName labels the keys at the top of the file. It is not part of any key.
	RootSectionName = "node_base"
)

// notWritableInThisFile are root paths the root section does not declare: the home directory comes from
// the command line, and the node mode is the input a resolution is asked about.
var notWritableInThisFile = []string{filledFromTheCommandLine, statedAtTheTopOfTheFile}

// statedAtTheTopOfTheFile is the node mode, which a generated file already states as node_mode.
const statedAtTheTopOfTheFile = "mode"

// removedFromTheNode are deprecated root paths nothing reads.
var removedFromTheNode = []string{"proxy-app", "abci", "filter-peers"}

// nodeRootSchema declares the root keys of the node's configuration file: tmcfg.Config without its
// tables, which register as their own sections. A test holds the extra fields against tmcfg.Config.
type nodeRootSchema struct {
	tmcfg.BaseConfig `mapstructure:",squash"`

	AutobahnConfigFile      string `mapstructure:"autobahn-config-file"`
	HashVaultDisabledUnsafe bool   `mapstructure:"hash-vault-disabled-unsafe"`
}

// removedSettings are deprecated consensus paths a written value cannot change.
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

// neverReachTheMempool are mempool paths no code reads, though a generated file writes them. The
// similarly spelled ttl-duration and ttl-num-blocks are live.
var neverReachTheMempool = []string{
	unreadAndUnmarked,
	"pending-ttl-duration",
	"pending-ttl-num-blocks",
}

// unreadAndUnmarked is the member of neverReachTheMempool whose field is not marked deprecated.
const unreadAndUnmarked = "max-batch-bytes"

// reachesNoReactor is the self-remediation path that is validated but read by no reactor. Its field is
// not marked deprecated.
const reachesNoReactor = "p2p-no-peers-available-window-seconds"

// fixedForEveryNode is the deprecated metric prefix; metrics always use a fixed one.
const fixedForEveryNode = "namespace"

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
		selfRemediationDefaults, reachesNoReactor)
	declareRootKeys(RootSectionName, &nodeRootSchema{}, rootDefaults,
		append(append([]string{}, notWritableInThisFile...), removedFromTheNode...)...)

	// Every section here is decoded rather than looked up.
	for _, name := range registeredHere {
		registry.DeclareDecodedNotLookedUp(name,
			"decoded into the node's own configuration struct by the boot's handler, which reads that "+
				"file once; nothing looks these keys up afterwards")
	}
}

// registeredHere are the sections this package registered, in order.
var registeredHere []string

// declareSection registers a section and records that it belongs to this package.
func declareSection(name string, prototype any, defaults func(registry.Mode) any, excluding ...string) {
	registry.RegisterSectionExcluding(name, prototype, defaults, excluding...)
	registeredHere = append(registeredHere, name)
}

// declareRootKeys registers root keys and records that the section belongs to this package.
func declareRootKeys(name string, prototype any, defaults func(registry.Mode) any, excluding ...string) {
	registry.RegisterRootKeysExcluding(name, prototype, defaults, excluding...)
	registeredHere = append(registeredHere, name)
}

// forMode is the configuration seid init writes for a node of this kind, apart from
// filledByTheGenerator: the node's defaults with the mode rules applied.
func forMode(mode registry.Mode) *tmcfg.Config {
	out := tmcfg.DefaultConfig()
	// The mode rules read the mode from the config.
	out.Mode = string(mode)
	params.SetTendermintConfigByMode(out)
	return out
}

// filledFromTheCommandLine is the home path several sections carry. The node sets it from the command
// line after reading the file, so declaring it would deliver an empty home over the real one.
const filledFromTheCommandLine = "home"

// derivedFromTheConnectionLimit is an optional ceiling whose default is unset, meaning the node derives
// it from the total connection limit. There is no value to declare as its default.
const derivedFromTheConnectionLimit = "max-outbound-connections"

// readByNothing is a testing parameter no code reads.
const readByNothing = "test-dial-fail"

// filledByTheGenerator are declared keys seid init sets from its inputs (chain ID, moniker) rather than
// the mode, so their declared defaults are not what a generated file carries. moniker's default is the
// host name.
var filledByTheGenerator = []string{P2PSectionName + ".bootstrap-peers", "moniker"}

// p2pDefaults is the peer-to-peer section's default for a mode.
func p2pDefaults(mode registry.Mode) any { return *forMode(mode).P2P }

// rpcDefaults is the RPC section's default for a mode. Only the listen address varies.
func rpcDefaults(mode registry.Mode) any { return *forMode(mode).RPC }

// consensusDefaults is the consensus section's default, the same for every mode.
func consensusDefaults(mode registry.Mode) any { return *forMode(mode).Consensus }

// mempoolDefaults is the mempool section's default, the same for every mode.
func mempoolDefaults(mode registry.Mode) any { return *forMode(mode).Mempool }

// stateSyncDefaults is the state sync section's default, the same for every mode.
func stateSyncDefaults(mode registry.Mode) any { return *forMode(mode).StateSync }

// txIndexDefaults is the transaction index section's default for a mode. Only the indexer varies.
func txIndexDefaults(mode registry.Mode) any { return *forMode(mode).TxIndex }

// instrumentationDefaults is the instrumentation section's default, the same for every mode.
func instrumentationDefaults(mode registry.Mode) any { return *forMode(mode).Instrumentation }

// privValidatorDefaults is the signing key section's default, the same for every mode.
func privValidatorDefaults(mode registry.Mode) any { return *forMode(mode).PrivValidator }

// rootDefaults is the root keys' default, the same for every mode.
func rootDefaults(mode registry.Mode) any {
	live := forMode(mode)
	return nodeRootSchema{
		BaseConfig:              live.BaseConfig,
		AutobahnConfigFile:      live.AutobahnConfigFile,
		HashVaultDisabledUnsafe: live.HashVaultDisabledUnsafe,
	}
}

// selfRemediationDefaults is the self-remediation section's default, the same for every mode.
func selfRemediationDefaults(mode registry.Mode) any { return *forMode(mode).SelfRemediation }
