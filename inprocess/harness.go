//go:build inprocess

package inprocess

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	dbm "github.com/tendermint/tm-db"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
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

// Options configures a Start. The zero value is invalid (Validators must be
// >= 1); use sensible explicit values.
type Options struct {
	// Validators is the number of in-process validators (>= 1). Each is a full
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
	// the bridge the YAML runner's in-process arm needs: the bank suite signs as
	// `admin` (node 0) and the docker topology also seeds `node_admin` per node.
	ExtraKeys []ExtraKey
}

// ExtraKey is a non-validator genesis account the harness creates and funds. It
// mirrors the docker localnode topology where `admin` lives on node 0 only and
// `node_admin` exists per node, so suites that sign as those names run unchanged
// against the in-process arm.
type ExtraKey struct {
	// Name is the keyring key name (e.g. "admin", "node_admin").
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

	app      *app.App
	tmNode   rpclocal.NodeService
	rpc      *rpclocal.Local
	serveErr chan error // EVM listener Start() failures (recipe: no process-wide panic)
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

// Start stands up opts.Validators in-process validators, wires a full P2P mesh,
// starts each node's RPC + EVM listeners, and returns once every node is
// constructed and started (NOT once consensus is live — call WaitReady for that).
//
// On any error mid-bring-up, every already-started node is torn down before
// returning, so a partial failure leaks nothing. The caller still must Close the
// returned Network on the success path; Start does not register cleanup.
func Start(ctx context.Context, opts Options) (_ *Network, retErr error) {
	opts = opts.withDefaults()
	if opts.Validators < 1 {
		return nil, fmt.Errorf("inprocess: Options.Validators must be >= 1, got %d", opts.Validators)
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
		codec:     enc.Marshaler,
		txConfig:  enc.TxConfig,
		chainID:   opts.ChainID,
		bondDenom: sdk.DefaultBondDenom,
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
		// this node's loopback TM RPC. The in-process runner arm still injects the
		// same values as flags defensively, but pinning them here keeps opaque
		// sourced helper scripts (which call bare `seid`) on the right node.
		writeClientConfig(filepath.Join(nodeDir, "config/client.toml"), net.opts.ChainID, addrs.rpcAddr)

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
			serveErr: make(chan error, 2), // one HTTP + one WS listener
		})
	}
	return nil
}

// provisionExtraKeys creates each Options.ExtraKey in its target node's home
// `test` keyring and funds its genesis account. It runs after provisionNodes (so
// every node's keyring exists) and before genesis assembly (so the balances fold
// into the base genesis). This is the keyring/home bridge the YAML runner's
// in-process arm relies on — `admin` on node 0, `node_admin` per node — matching
// the docker localnode topology so bank suites sign unchanged.
func (net *Network) provisionExtraKeys(gb *genesisBuilder) error {
	algoStr := string(hdSecp256k1())
	for _, ek := range net.opts.ExtraKeys {
		if ek.Node < 0 || ek.Node >= len(net.nodes) {
			return fmt.Errorf("extra key %q targets node %d, out of range [0,%d)", ek.Name, ek.Node, len(net.nodes))
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

// startNode builds the app, constructs + starts the tendermint node with an
// EMPTY-valset genesis (recipe #1), wires the local RPC client, and registers
// the EVM listeners. The node's EVM Start() failures land on n.serveErr instead
// of panicking (recipe: a single bind failure must not kill all N nodes).
func (net *Network) startNode(ctx context.Context, n *node, enc encoding) error {
	theApp := newNodeApp(n, enc)
	theApp.SetEVMServeErr(n.serveErr)
	n.app = theApp

	// recipe #1: zero the validator set so CometBFT derives it from InitChain.
	// genesis.go writes Validators=nil at genesis-build time; this re-asserts the
	// invariant against the file round-trip here (collectGentxs rewrites the
	// genesis via ExportGenesisFileWithTime, so re-read it defensively).
	genDoc, err := tmtypes.GenesisDocFromFile(n.tmCfg.GenesisFile())
	if err != nil {
		return err
	}
	genDoc.Validators = nil

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

// buildNodeConfig builds an isolated per-node tendermint config with loopback TM
// RPC / P2P listeners and the conn-tracker ceiling raised (recipes #4, #5, #6).
// EVM bind-host is not config-scopable (evmrpc hardcodes 0.0.0.0); the EVM ports
// are allocated free here and dialed via loopback.
func buildNodeConfig(nodeDir, moniker string, timeoutCommit time.Duration) (*config.Config, nodeAddrs, error) {
	sctx := server.NewDefaultContext()
	tmCfg := sctx.Config
	tmCfg.Mode = config.ModeValidator
	tmCfg.Moniker = moniker
	tmCfg.SetRoot(nodeDir)
	tmCfg.Consensus.UnsafeCommitTimeoutOverride = timeoutCommit
	tmCfg.TxIndex = config.TestTxIndexConfig()
	// recipe #6: loopback collapses every peer onto 127.0.0.1, so the router's
	// IP-keyed conn-tracker counts all N-1 inbound on one key. AllowDuplicateIP
	// is a peer-manager flag and does NOT touch the router conn-tracker.
	tmCfg.P2P.MaxIncomingConnectionAttempts = 10000
	tmCfg.P2P.AllowDuplicateIP = true
	// recipe #4: metrics-off avoids the prometheus.DefaultRegisterer dup panic
	// from the process-wide registries. Invariant: this must stay off until the
	// evmrpc/EVM-keeper metrics are de-globalized — re-enabling Prometheus
	// without that reintroduces the panic.
	tmCfg.Instrumentation.Prometheus = false

	// recipe #5: server.FreeTCPAddr composes tcp://0.0.0.0:PORT — a publicly-bound
	// listener. An in-process harness must scope every listener to loopback, so we
	// take only the free port and compose the 127.0.0.1 address ourselves.
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
func newNodeApp(n *node, enc encoding) *app.App {
	return app.New(
		dbm.NewMemDB(),
		io.Discard,
		true,
		map[int64]bool{},
		n.home,
		1,
		false,
		n.tmCfg,
		enc,
		wasm.EnableAllProposals,
		appOptions{chainID: n.clientCx.ChainID, httpPort: n.httpPort, wsPort: n.wsPort},
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
// broadcast with -b sync and poll on-chain side effects. Best-effort: a failed
// write leaves the in-process arm's explicit per-command flags as the fallback.
func writeClientConfig(path, chainID, rpcAddr string) {
	_ = os.WriteFile(path, []byte(fmt.Sprintf(clientConfigTemplate, chainID, rpcAddr)), 0o600)
}

// freePort allocates a free loopback TCP port via server.FreeTCPAddr.
func freePort() (int, error) {
	_, portStr, err := server.FreeTCPAddr()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(portStr)
}
