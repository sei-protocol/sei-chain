//go:build inprocess

package inprocess

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	dbm "github.com/tendermint/tm-db"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	cryptocodec "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	tmnode "github.com/sei-protocol/sei-chain/sei-tendermint/node"
	rpclocal "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/local"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
)

// chainIDPrefix prefixes every harness-generated chain-id. The value is free —
// the harness signs its own txs with Options.ChainID, and it is NOT the EVM
// chain ID (the keeper derives that). A fresh token per Start mirrors the
// controller harness's runChainID discipline: a static id reused across runs
// collides with a prior run's persisted genesis and halts at height 1.
const chainIDPrefix = "sei-inprocess"

// freshChainID returns a unique chain-id token (chainIDPrefix-<8 hex>). Falls
// back to a nanosecond timestamp if crypto/rand is unavailable, which still
// yields a distinct id per Start.
func freshChainID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%d", chainIDPrefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%x", chainIDPrefix, b[:])
}

// Options configures a Start. The zero value is invalid (Validators must be 1
// or >= 3; 2 is rejected — see the Validators doc); use explicit values.
type Options struct {
	// Validators is the number of in-process validators. Valid: 1 or >= 3. 2 is
	// REJECTED — two validators each have exactly one peer, and CometBFT's
	// BlockPool.IsCaughtUp requires >1 peer, so an N=2 mesh deadlocks in
	// block-sync. N=1 runs as a solo proposer (onlyValidatorIsUs skips
	// block-sync); N>=3 gives every node >=2 peers. Each validator is a full
	// (app, node.New) pair serving its own RPC stack.
	Validators int

	// ChainID is the genesis chain id; "" generates a fresh per-run id
	// (chainIDPrefix-<rand>) so a run never collides with a prior run's genesis.
	// Set it explicitly only when a test pins a specific chain id.
	ChainID string

	// BaseDir is the parent dir for per-node homes; "" creates a temp dir the
	// harness owns and removes at Close. A caller-supplied BaseDir is NOT removed.
	BaseDir string

	// TimeoutCommit is the consensus commit timeout; 0 defaults to 2s. The
	// dominant cadence lever — lower it (e.g. 500ms) for faster tests.
	TimeoutCommit time.Duration

	// ExtraKeys are non-validator genesis accounts to create + fund. Each key is
	// written into its target node's home `test` keyring (so a host `seid --home
	// <home> --keyring-backend test` resolves it) and funded at genesis. This is
	// the bridge the YAML runner's in-process arm needs — e.g. the bank suite signs
	// as `admin` on node 0. It must NOT seed `node_admin`: that name is the per-node
	// validator operator the harness provisions (see operatorKeyName), and
	// provisionExtraKeys rejects it.
	ExtraKeys []ExtraKey

	// GenesisUseiSupply, when non-nil, pins total genesis usei supply to this exact
	// amount: after validators + ExtraKeys are funded, a fixed keyless reserve account
	// is credited (target - sum of existing usei), decoupling supply-parity from
	// validator count. nil = supply is just the sum of funded balances (current behavior).
	GenesisUseiSupply *sdk.Int

	// MintTokenReleaseSchedule, when non-empty, writes the mint token_release_schedule
	// (dates as day-offsets from genesis, mint_denom pinned to usei) and pins the epoch
	// clock so the first release fires on the schedule's start date. Empty = mint stays
	// at its module default.
	MintTokenReleaseSchedule []MintRelease

	// GovParams, when non-nil, overrides the gov module's voting/deposit/tally params.
	// Required for the gov suites: the default 2-day voting period never resolves their
	// short-sleep vote cases.
	GovParams *GovParams

	// AppConfigOverride pins app-config values the harness would otherwise leave at
	// their appOptions.Get default, keyed by the SAME flag constants production reads
	// (e.g. gigaconfig.FlagEnabled, gigaconfig.FlagOCCEnabled). A present key wins over
	// the default; an absent key falls through to it; nil = pure harness defaults. This
	// makes an implicit default (e.g. giga-on via gigaconfig.DefaultConfig) load-bearing:
	// a test can pin it so a later default flip fails the pin instead of silently
	// downgrading coverage.
	//
	// The override is NETWORK-WIDE — every node is built with the same map, so it cannot
	// express a heterogeneous mesh (one giga node, one V2). That is not a missing feature
	// but a hard invariant: SkipLastResultsHashValidation (sei-tendermint/types/block.go)
	// is a process-global atomic that app.New sets from the giga flag, so two nodes with
	// different giga settings in one process would clobber each other's validation mode.
	//
	// A value whose type ReadConfig cannot cast (e.g. a string where a bool is wanted)
	// surfaces as a panic at construction — acceptable fail-fast for a test seam. Keys
	// the harness itself owns are rejected loudly; see appConfigDenylisted.
	AppConfigOverride map[string]any
}

// appConfigDenylisted reports whether key is one appOptions.Get pins — a load-bearing
// harness invariant (chain-id signing, the EVM listeners + stats tracker, SeiDB
// enable/backend/snapshot), not a tunable an AppConfigOverride may clobber. Derived from
// Get itself: a zero-value appOptions returns non-nil for every pinned key, so the guard
// can never drift from the pins the way a hand-maintained list did.
func appConfigDenylisted(key string) bool {
	return appOptions{}.Get(key) != nil
}

// validateAppConfigOverride rejects an override reaching for a harness-owned key. It runs
// at construction (before any node is built) and returns an error — the same recoverable
// Options-validation contract as the Validators check, not a panic.
func validateAppConfigOverride(override map[string]any) error {
	for key := range override {
		if appConfigDenylisted(key) {
			return fmt.Errorf("inprocess: AppConfigOverride key %q is harness-owned and cannot be overridden", key)
		}
	}
	return nil
}

// MintRelease is one token_release_schedule entry, expressed as day-offsets from the
// network's genesis date. Offsets (not absolute date strings) let the harness derive
// the schedule and the epoch clock from one reference instant, so the first release
// fires on the schedule's start date deterministically.
type MintRelease struct {
	StartDaysFromGenesis int     // 0 = genesis day
	EndDaysFromGenesis   int     // must be > StartDaysFromGenesis
	Amount               sdk.Int // token_release_amount
}

// GovParams overrides the gov module genesis params. nil leaves the module default.
type GovParams struct {
	VotingPeriod          time.Duration
	ExpeditedVotingPeriod time.Duration
	MaxDepositPeriod      time.Duration
	Quorum                sdk.Dec
	Threshold             sdk.Dec
	ExpeditedQuorum       sdk.Dec
	ExpeditedThreshold    sdk.Dec
}

// DockerLocalnodeGovParams returns the gov params the docker localnode sets
// (step2_genesis.sh): 0.5/0.5 normal quorum/threshold, 0.9/0.9 expedited, and
// 30s/15s/100s voting/expedited/deposit periods — what the gov suites resolve against.
func DockerLocalnodeGovParams() *GovParams {
	return &GovParams{
		VotingPeriod:          30 * time.Second,
		ExpeditedVotingPeriod: 15 * time.Second,
		MaxDepositPeriod:      100 * time.Second,
		Quorum:                sdk.MustNewDecFromStr("0.5"),
		Threshold:             sdk.MustNewDecFromStr("0.5"),
		ExpeditedQuorum:       sdk.MustNewDecFromStr("0.9"),
		ExpeditedThreshold:    sdk.MustNewDecFromStr("0.9"),
	}
}

// ExtraKey is a non-validator genesis account the harness creates and funds — a
// suite signing key the docker localnode seeds (e.g. `admin` on node 0), so suites
// that sign as those names run unchanged against the in-process arm. The validator
// operator `node_admin` is NOT an ExtraKey: the harness provisions it per node
// (see operatorKeyName), and provisionExtraKeys rejects it.
type ExtraKey struct {
	// Name is the keyring key name (e.g. "admin"); must not be operatorKeyName.
	Name string
	// Node is the 0-based validator index whose home keyring receives the key.
	Node int
	// Coins is the genesis balance for the key's account. Empty funds nothing
	// (the account still exists), which is rarely what a signing key wants.
	Coins sdk.Coins
}

func (o Options) withDefaults() Options {
	if o.ChainID == "" {
		o.ChainID = freshChainID()
	}
	if o.TimeoutCommit == 0 {
		o.TimeoutCommit = 2 * time.Second
	}
	return o
}

// node is one in-process validator: its identity, listener addresses, app, and
// running tendermint service. Exported access is via the *Node handle (handle.go)
// so the running internals stay encapsulated.
type node struct {
	moniker  string
	nodeID   string
	pubKey   cryptotypes.PubKey
	addr     sdk.AccAddress
	home     string
	tmCfg    *config.Config
	clientCx client.Context

	p2pHost  string
	p2pPort  string
	rpcAddr  string // tcp://127.0.0.1:PORT (TM RPC listen address)
	httpPort int    // EVM JSON-RPC HTTP
	wsPort   int    // EVM JSON-RPC WS

	app    *app.App
	tmNode rpclocal.NodeService
	rpc    *rpclocal.Local
}

// Network is a handle to a running in-process mesh. It owns the lifecycle: Close
// tears every node down deterministically. Not goroutine-safe across calls.
type Network struct {
	opts       Options
	baseDir    string
	ownBaseDir bool // true => Close removes baseDir
	nodes      []*node
	closed     bool
}

// networkStarted enforces one in-process network per process. app.New wires the
// EVM worker pool, the metrics printer, and Prometheus registries — all
// process-global singletons created via sync.Once that never cleanly re-init, so
// a second Start would silently inherit a closed/dead set. Start fails loudly
// instead. Never reset (not even on Close): the singletons cannot be revived.
var networkStarted atomic.Bool

// Start stands up opts.Validators in-process validators, starts each node's RPC
// + EVM listeners, and returns once every node is constructed and started (NOT
// once consensus is live — call WaitReady for that).
//
// The P2P mesh is not wired by Start directly. It is derived per the
// gentx-derived peer mesh invariant (see doc.go): collectGentxs →
// genutil.GenAppStateFromConfig mutates each node's tmCfg.P2P.PersistentPeers in
// place from the gentx memos. Start guards that this implicit wiring actually
// happened (see the assertion after collectGentxs) so a refactor that breaks it
// fails loudly instead of silently dropping consensus.
//
// On any error mid-bring-up, every already-started node is torn down before
// returning, so a partial failure leaks nothing. The caller still must Close the
// returned Network on the success path; Start does not register cleanup.
func Start(ctx context.Context, opts Options) (_ *Network, retErr error) {
	opts = opts.withDefaults()
	if opts.Validators < 1 {
		return nil, fmt.Errorf("inprocess: Options.Validators must be 1 or >= 3, got %d", opts.Validators)
	}
	// N=2 deadlocks in CometBFT block-sync: each node has exactly 1 peer, and
	// BlockPool.IsCaughtUp (sei-tendermint internal/blocksync/pool.go) hard-requires
	// >1 peer to ever report caught-up, so neither node leaves block-sync. Reject it
	// loudly rather than hang. N=1 (solo proposer via onlyValidatorIsUs) and N>=3
	// (>=2 peers each) both work — see startNode and doc.go.
	if opts.Validators == 2 {
		return nil, fmt.Errorf("inprocess: Options.Validators == 2 deadlocks in CometBFT block-sync (BlockPool.IsCaughtUp requires >1 peer); use 1 or >= 3")
	}
	// Reject a harness-owned override before any node is built — fail-loud at the
	// public seam rather than mid-bring-up.
	if err := validateAppConfigOverride(opts.AppConfigOverride); err != nil {
		return nil, err
	}

	baseDir, ownBaseDir, err := resolveBaseDir(opts.BaseDir)
	if err != nil {
		return nil, err
	}
	net := &Network{opts: opts, baseDir: baseDir, ownBaseDir: ownBaseDir}
	// Any error past this point tears down whatever came up (including the temp
	// dir we own) so the caller never holds a half-built Network.
	defer func() {
		if retErr != nil {
			net.Close()
		}
	}()

	enc := app.MakeEncodingConfig()
	gb := &genesisBuilder{
		codec:            enc.Marshaler,
		txConfig:         enc.TxConfig,
		chainID:          opts.ChainID,
		bondDenom:        sdk.DefaultBondDenom,
		useiSupplyTarget: opts.GenesisUseiSupply,
		mintSchedule:     opts.MintTokenReleaseSchedule,
		govParams:        opts.GovParams,
		mintRef:          time.Now().UTC(), // one reference instant for the mint schedule + epoch clock
	}

	if err := net.provisionNodes(enc, gb); err != nil {
		return nil, err
	}
	if err := net.provisionExtraKeys(gb); err != nil {
		return nil, err
	}

	baseState := app.ModuleBasics.DefaultGenesis(enc.Marshaler)
	genFiles := make([]string, len(net.nodes))
	for i, n := range net.nodes {
		genFiles[i] = n.tmCfg.GenesisFile()
	}
	if err := gb.writeBaseGenesis(baseState, genFiles); err != nil {
		return nil, fmt.Errorf("write base genesis: %w", err)
	}
	if err := gb.collectGentxs(net.nodes, filepath.Join(baseDir, "gentxs")); err != nil {
		return nil, fmt.Errorf("collect gentxs: %w", err)
	}
	// gentx-derived peer mesh guard: collectGentxs is what populates each node's
	// PersistentPeers (in place, via GenAppStateFromConfig — see doc.go). For N>=2
	// an empty PersistentPeers means the implicit wiring did not land and consensus
	// will never form; fail loudly here rather than hang in WaitReady.
	if len(net.nodes) >= 2 {
		for _, n := range net.nodes {
			if n.tmCfg.P2P.PersistentPeers == "" {
				return nil, fmt.Errorf(
					"inprocess: gentx-derived peer mesh not wired: collectGentxs did not populate PersistentPeers for %s — did a refactor clone or reorder the config?",
					n.moniker,
				)
			}
		}
	}

	// One network per process (see networkStarted): claim the slot here, right
	// before the first app.New (in startNode) — the first point that touches the
	// process-global EVM singletons. Every step above (base dir, provisioning,
	// genesis) can fail recoverably without burning the slot. Once claimed it is
	// never released: app.New's singletons can't re-init.
	if !networkStarted.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("inprocess: a network was already started in this process; only one is supported (EVM worker pool / metrics printer / Prometheus registries are process-global singletons)")
	}
	for _, n := range net.nodes {
		if err := net.startNode(ctx, n, enc); err != nil {
			return nil, fmt.Errorf("start %s: %w", n.moniker, err)
		}
	}
	return net, nil
}

// provisionNodes runs the first pass: per-node keys, node IDs, gentxs, isolated
// tendermint config, and loopback port allocation. It populates net.nodes.
func (net *Network) provisionNodes(enc encoding, gb *genesisBuilder) error {
	algoStr := string(hdSecp256k1())
	for i := 0; i < net.opts.Validators; i++ {
		moniker := fmt.Sprintf("node%d", i)
		nodeDir := filepath.Join(net.baseDir, moniker, "simd")
		// The keyring lives in the node home (not a separate simcli dir) so a host
		// `seid --home <nodeDir> --keyring-backend test` — how the YAML runner's
		// in-process arm targets a node — resolves the same keys this harness wrote
		// (keyring dir falls back to --home; see client/cmd.go).
		clientDir := nodeDir
		if err := os.MkdirAll(filepath.Join(nodeDir, "config"), 0o750); err != nil {
			return err
		}

		tmCfg, addrs, err := buildNodeConfig(nodeDir, moniker, net.opts.TimeoutCommit)
		if err != nil {
			return err
		}

		nodeID, pubKey, err := genutil.InitializeNodeValidatorFiles(tmCfg)
		if err != nil {
			return fmt.Errorf("init validator files for %s: %w", moniker, err)
		}

		kb, err := keyring.New(sdk.KeyringServiceName(), keyring.BackendTest, clientDir, nil)
		if err != nil {
			return err
		}
		algos, _ := kb.SupportedAlgorithms()
		algo, err := keyring.NewSigningAlgoFromString(algoStr, algos)
		if err != nil {
			return err
		}

		addr, err := gb.fundValidator(
			kb, moniker, pubKey, algo,
			consensusTokens(1000), consensusTokens(500), consensusTokens(100),
			addrs.p2pHost, addrs.p2pPort, nodeID, filepath.Join(net.baseDir, "gentxs"),
		)
		if err != nil {
			return err
		}

		writeAppConfig(filepath.Join(nodeDir, "config/app.toml"))
		// Seed a client.toml so a bare host `seid --home <nodeDir>` (no per-command
		// flags) already targets this node: test keyring, the harness chain-id, and
		// this node's loopback TM RPC. The runner arm's shim also injects --home and
		// --node explicitly (so RPC targeting does not rest on this file alone), but
		// keyring-backend=test is resolved ONLY from here — the sourced helpers pass
		// no --keyring-backend flag — so this write is load-bearing and its failure
		// must surface.
		if err := writeClientConfig(filepath.Join(nodeDir, "config/client.toml"), net.opts.ChainID, addrs.rpcAddr); err != nil {
			return fmt.Errorf("write client.toml for %s: %w", moniker, err)
		}

		clientCx := client.Context{}.
			WithKeyringDir(clientDir).WithKeyring(kb).WithHomeDir(tmCfg.RootDir).
			WithChainID(net.opts.ChainID).WithInterfaceRegistry(enc.InterfaceRegistry).
			WithCodec(enc.Marshaler).WithLegacyAmino(enc.Amino).
			WithTxConfig(enc.TxConfig).WithAccountRetriever(accountRetriever())

		net.nodes = append(net.nodes, &node{
			moniker: moniker, nodeID: nodeID, pubKey: pubKey, addr: addr,
			home: nodeDir, tmCfg: tmCfg, clientCx: clientCx,
			p2pHost: addrs.p2pHost, p2pPort: addrs.p2pPort,
			rpcAddr:  addrs.rpcAddr,
			httpPort: addrs.httpPort, wsPort: addrs.wsPort,
		})
	}
	return nil
}

// provisionExtraKeys creates each Options.ExtraKey in its target node's home
// `test` keyring and funds its genesis account. It runs after provisionNodes (so
// every node's keyring exists) and before genesis assembly (so the balances fold
// into the base genesis). This is the keyring/home bridge the YAML runner's
// in-process arm relies on — e.g. `admin` on node 0 — matching the docker
// localnode signing keys so suites sign unchanged. (node_admin is not seeded
// here; it is the per-node operator provisioned in provisionNodes.)
func (net *Network) provisionExtraKeys(gb *genesisBuilder) error {
	algoStr := string(hdSecp256k1())
	for _, ek := range net.opts.ExtraKeys {
		if ek.Node < 0 || ek.Node >= len(net.nodes) {
			return fmt.Errorf("extra key %q targets node %d, out of range [0,%d)", ek.Name, ek.Node, len(net.nodes))
		}
		// operatorKeyName is provisioned per node (fundValidator); an ExtraKey reusing
		// it would overwrite the operator with a plain account — fail-loud rather than
		// silently corrupt the operator identity.
		if ek.Name == operatorKeyName {
			return fmt.Errorf("inprocess: ExtraKey name %q is reserved for the per-node validator operator; use a different name", ek.Name)
		}
		kb := net.nodes[ek.Node].clientCx.Keyring
		algos, _ := kb.SupportedAlgorithms()
		algo, err := keyring.NewSigningAlgoFromString(algoStr, algos)
		if err != nil {
			return err
		}
		if err := gb.fundAccount(kb, ek.Name, algo, ek.Coins); err != nil {
			return fmt.Errorf("provision extra key %q on node%d: %w", ek.Name, ek.Node, err)
		}
	}
	return nil
}

// startNode builds the app, constructs + starts the tendermint node, wires the
// local RPC client, and registers the EVM listeners. The genesis valset is
// N-dependent per the empty-valset invariant — see the N=1 exception below.
func (net *Network) startNode(ctx context.Context, n *node, enc encoding) error {
	theApp := newNodeApp(n, enc, net.opts.AppConfigOverride)
	n.app = theApp

	// empty-valset invariant (N>=2): zero the validator set so every node derives
	// the valset from its own InitChain response — without this, multi-node
	// consensus replay fails. genesis.go writes Validators=nil at build time;
	// re-assert it here against the collectGentxs file round-trip
	// (ExportGenesisFileWithTime).
	//
	// N=1 EXCEPTION: a sole validator must skip block-sync and produce blocks as
	// solo proposer, which only happens when sei-tendermint's onlyValidatorIsUs
	// (node/setup.go) sees state.Validators.Size()==1 with our consensus key at
	// the blockSync decision (node/node.go: `blockSync := !onlyValidatorIsUs`).
	// That decision reads the genesis-derived state (MakeGenesisState) BEFORE
	// InitChain runs, so an empty valset leaves size 0, onlyValidatorIsUs returns
	// false, and the node enters block-sync — where BlockPool.IsCaughtUp requires
	// >1 peer (pool.go) and a 0-peer solo node hangs forever at height 1. Pinning
	// the single validator into genesis here makes onlyValidatorIsUs fire.
	genDoc, err := tmtypes.GenesisDocFromFile(n.tmCfg.GenesisFile())
	if err != nil {
		return err
	}
	genDoc.Validators = nil
	if len(net.nodes) == 1 {
		tmPub, perr := cryptocodec.ToTmPubKeyInterface(n.pubKey)
		if perr != nil {
			return fmt.Errorf("convert consensus pubkey for %s: %w", n.moniker, perr)
		}
		genDoc.Validators = []tmtypes.GenesisValidator{
			{PubKey: tmPub, Address: tmPub.Address(), Name: n.moniker, Power: 100},
		}
	}
	// Match the docker localnode's block-gas pair — 35M max_gas / 70M max_gas_wanted
	// (its 2x ratio avoids false-positive gas rejections). The defaults are unlimited
	// max_gas (which the EVM reports as a 100M block gaslimit) and 50M max_gas_wanted
	// (too tight for the 35M cap — a batch whose declared gas exceeds 50M but used gas
	// stays under 35M would fail ProcessProposal's checkTotalBlockGas). Set here, on the
	// genDoc the node starts with, because collectGentxs's re-export resets ConsensusParams.
	if genDoc.ConsensusParams == nil {
		genDoc.ConsensusParams = tmtypes.DefaultConsensusParams()
	}
	genDoc.ConsensusParams.Block.MaxGas = 35_000_000
	genDoc.ConsensusParams.Block.MaxGasWanted = 70_000_000

	tmNode, err := tmnode.New(
		ctx, n.tmCfg, func() {}, theApp, genDoc,
		[]trace.TracerProviderOption{}, tmnode.NoOpMetricsProvider(),
		tmtypes.DefaultConsensusPolicy(),
	)
	if err != nil {
		return fmt.Errorf("node.New: %w", err)
	}
	n.tmNode = tmNode
	if err := tmNode.Start(ctx); err != nil {
		return fmt.Errorf("node.Start: %w", err)
	}

	lc, err := rpclocal.New(tmNode)
	if err != nil {
		return err
	}
	n.rpc = lc
	n.clientCx = n.clientCx.WithClient(lc)
	// RegisterLocalServices builds the EVM HTTP/WS listeners; their goroutines
	// block on the first-block start signal. (It also registers query/tx services
	// on the in-process gRPC query router, but the harness starts no standalone
	// cosmos gRPC listener — TM RPC + EVM are the served surface.)
	theApp.RegisterLocalServices(lc, n.clientCx.TxConfig)
	return nil
}

// resolveBaseDir returns the base dir for node homes and whether the harness owns
// it (and so must remove it at Close).
func resolveBaseDir(dir string) (string, bool, error) {
	if dir != "" {
		return dir, false, nil
	}
	tmp, err := os.MkdirTemp("", "sei-inprocess-")
	if err != nil {
		return "", false, fmt.Errorf("create base dir: %w", err)
	}
	return tmp, true, nil
}

// nodeAddrs holds one node's loopback listener addresses.
type nodeAddrs struct {
	p2pHost, p2pPort string
	rpcAddr          string
	httpPort, wsPort int
}

// buildNodeConfig builds an isolated per-node tendermint config: metrics off
// (metrics-off constraint), loopback TM RPC / P2P listeners (loopback bind
// scope), and the conn-tracker ceiling raised (loopback conn-tracker ceiling).
// EVM bind-host is not config-scopable (evmrpc hardcodes 0.0.0.0); the EVM ports
// are allocated free here and dialed via loopback (the 0.0.0.0 EVM caveat).
func buildNodeConfig(nodeDir, moniker string, timeoutCommit time.Duration) (*config.Config, nodeAddrs, error) {
	sctx := server.NewDefaultContext()
	tmCfg := sctx.Config
	tmCfg.Mode = config.ModeValidator
	tmCfg.Moniker = moniker
	tmCfg.SetRoot(nodeDir)
	tmCfg.Consensus.UnsafeCommitTimeoutOverride = timeoutCommit
	// Force the kv tx-index sink on. `-b block` (BroadcastTxCommit) polls the tx
	// index, not the event bus, so YAML suites using it (wasm) hang without this —
	// a mode-based config would give a validator the null indexer.
	tmCfg.TxIndex = config.TestTxIndexConfig()
	// loopback conn-tracker ceiling: loopback collapses every peer onto 127.0.0.1,
	// so the router's IP-keyed conn-tracker counts all N-1 inbound on one key.
	// AllowDuplicateIP is a peer-manager flag and does NOT touch the router
	// conn-tracker.
	tmCfg.P2P.MaxIncomingConnectionAttempts = 10000
	tmCfg.P2P.AllowDuplicateIP = true
	// metrics-off constraint: metrics-off avoids the prometheus.DefaultRegisterer
	// dup panic from the process-wide registries. This must stay off until the
	// evmrpc/EVM-keeper metrics are de-globalized — re-enabling Prometheus without
	// that reintroduces the panic.
	tmCfg.Instrumentation.Prometheus = false

	// loopback bind scope: freePort returns a bare port (probed free on 127.0.0.1);
	// we compose the tcp://127.0.0.1 address ourselves so every TM listener is
	// loopback-scoped rather than the default all-interfaces bind.
	var a nodeAddrs
	rpcPort, err := freePort()
	if err != nil {
		return nil, a, err
	}
	a.rpcAddr = fmt.Sprintf("tcp://127.0.0.1:%d", rpcPort)
	tmCfg.RPC.ListenAddress = a.rpcAddr

	p2pPort, err := freePort()
	if err != nil {
		return nil, a, err
	}
	a.p2pHost = "127.0.0.1"
	a.p2pPort = strconv.Itoa(p2pPort)
	tmCfg.P2P.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", p2pPort)

	if a.httpPort, err = freePort(); err != nil {
		return nil, a, err
	}
	if a.wsPort, err = freePort(); err != nil {
		return nil, a, err
	}
	return tmCfg, a, nil
}

// newNodeApp builds a real sei-chain app for one node with EVM serving on its
// per-node ports against an in-memory DB and on-disk home.
func newNodeApp(n *node, enc encoding, appConfig map[string]any) *app.App {
	return app.New(
		dbm.NewMemDB(),
		io.Discard,
		true,
		map[int64]bool{},
		n.home,
		1,
		// enableCustomEVMPrecompiles: register the custom EVM precompiles (0x...100x).
		// Off, they are never registered and every precompile call is a silent no-op
		// (tx calls return a status-1 receipt with no state change, eth_call reads
		// return 0x) — the precompile/interop suites pass green while testing nothing.
		// This aligns precompile registration with the production start path only;
		// other app.New args here still diverge (baseapp OCC is off, wasm uses the
		// default gas register), so it is not full production parity.
		true,
		n.tmCfg,
		enc,
		wasm.EnableAllProposals,
		appOptions{chainID: n.clientCx.ChainID, httpPort: n.httpPort, wsPort: n.wsPort, appConfig: appConfig},
		app.EmptyWasmOpts,
		nil,
	)
}

// writeAppConfig writes a minimal per-node app.toml. The harness serves TM RPC +
// EVM (HTTP/WS) only; the cosmos gRPC server stays off (nothing in the harness
// path calls servergrpc.StartGRPCServer, so enabling it would advertise a port
// no listener binds).
func writeAppConfig(path string) {
	appCfg := srvconfig.DefaultConfig()
	// No gRPC listener is started; keep the written config consistent with that
	// and avoid an N>1 fixed-port collision if the standard start path is ever wired.
	appCfg.GRPC.Enable = false
	appCfg.GRPCWeb.Enable = false
	appCfg.Telemetry.Enabled = false
	srvconfig.WriteConfigFile(path, appCfg)
}

// clientConfigTemplate matches sei-cosmos client/config's client.toml schema. It
// is reproduced here (not imported) because that package's writer + config
// struct are unexported — the same reason genesis.go reimplements the network
// package's unexported helpers rather than forcing a cosmos source change.
const clientConfigTemplate = `chain-id = "%s"
keyring-backend = "test"
output = "json"
node = "%s"
broadcast-mode = "sync"
`

// writeClientConfig writes a client.toml pinning the test keyring, chain-id, and
// this node's loopback TM RPC so a bare host `seid --home <nodeDir>` already
// targets the node without per-command flags (client/config.ReadFromClientConfig
// reads <home>/config/client.toml). broadcast-mode stays sync — the suites
// broadcast with -b sync and poll on-chain side effects.
//
// This write is load-bearing, not best-effort: the sourced _tx_helpers.sh call
// bare `seid` with no --keyring-backend flag, so keyring-backend=test is resolved
// from this file (the shim only injects --home and --node). A failed write would
// silently fall the keyring back to the OS default and break signing — so the
// error is returned, not swallowed.
func writeClientConfig(path, chainID, rpcAddr string) error {
	return os.WriteFile(path, []byte(fmt.Sprintf(clientConfigTemplate, chainID, rpcAddr)), 0o600)
}

var (
	allocatedPortsMu sync.Mutex
	allocatedPorts   = map[int]struct{}{}
)

// freePort returns a TCP port free on the IPv4 loopback that this process has not
// already handed out. Two hazards make a bare "probe :0, close, return" flaky
// across the harness's 4*N allocations: on a dual-stack host "localhost" can
// resolve to ::1, verifying the port free on IPv6 while it stays bound on IPv4
// (independent namespaces) — so probe 127.0.0.1 explicitly; and two probes can
// return the same port intra-process — so the allocated set rejects a repeat. A
// bind-time race with an unrelated process is the only residual TOCTOU (see doc.go).
func freePort() (int, error) {
	allocatedPortsMu.Lock()
	defer allocatedPortsMu.Unlock()
	for attempt := 0; attempt < 100; attempt++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, fmt.Errorf("inprocess: allocate loopback port: %w", err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		if _, taken := allocatedPorts[port]; taken {
			continue
		}
		allocatedPorts[port] = struct{}{}
		return port, nil
	}
	return 0, fmt.Errorf("inprocess: no free loopback port after 100 attempts")
}
