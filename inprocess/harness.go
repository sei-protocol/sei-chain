//go:build inprocess

package inprocess

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// defaultChainID is the chain-id the sei-chain integration helpers and CLI
// hardcode; a different value fails every tx, so it is the default and not
// merely a placeholder.
const defaultChainID = "sei"

// Options configures a Start. The zero value is invalid (Validators must be
// >= 1); use sensible explicit values.
type Options struct {
	// Validators is the number of in-process validators (>= 1). Each is a full
	// (app, node.New) pair serving its own RPC stack.
	Validators int

	// ChainID is the genesis chain id; "" defaults to "sei".
	ChainID string

	// BaseDir is the parent dir for per-node homes; "" creates a temp dir the
	// harness owns and removes at Close. A caller-supplied BaseDir is NOT removed.
	BaseDir string

	// TimeoutCommit is the consensus commit timeout; 0 defaults to 2s. The
	// dominant cadence lever — lower it (e.g. 500ms) for faster tests.
	TimeoutCommit time.Duration
}

func (o Options) withDefaults() Options {
	if o.ChainID == "" {
		o.ChainID = defaultChainID
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
	grpcAddr string // 127.0.0.1:PORT
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
	wireMesh(net.nodes)

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
		clientDir := filepath.Join(net.baseDir, moniker, "simcli")
		if err := os.MkdirAll(filepath.Join(nodeDir, "config"), 0o750); err != nil {
			return err
		}
		if err := os.MkdirAll(clientDir, 0o750); err != nil {
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

		writeAppConfig(filepath.Join(nodeDir, "config/app.toml"), addrs.grpcAddr, net.opts)

		clientCx := client.Context{}.
			WithKeyringDir(clientDir).WithKeyring(kb).WithHomeDir(tmCfg.RootDir).
			WithChainID(net.opts.ChainID).WithInterfaceRegistry(enc.InterfaceRegistry).
			WithCodec(enc.Marshaler).WithLegacyAmino(enc.Amino).
			WithTxConfig(enc.TxConfig).WithAccountRetriever(accountRetriever())

		net.nodes = append(net.nodes, &node{
			moniker: moniker, nodeID: nodeID, pubKey: pubKey, addr: addr,
			home: nodeDir, tmCfg: tmCfg, clientCx: clientCx,
			p2pHost: addrs.p2pHost, p2pPort: addrs.p2pPort,
			rpcAddr: addrs.rpcAddr, grpcAddr: addrs.grpcAddr,
			httpPort: addrs.httpPort, wsPort: addrs.wsPort,
			serveErr: make(chan error, 2), // one HTTP + one WS listener
		})
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
	// RegisterLocalServices builds the EVM HTTP/WS listeners (their goroutines
	// block on the first-block start signal) and the gRPC tx service.
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
	grpcAddr         string
	httpPort, wsPort int
}

// buildNodeConfig builds an isolated per-node tendermint config with loopback
// listeners and the conn-tracker ceiling raised (recipes #4, #5, #6).
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
	// and lets the evmrpc/EVM-keeper metrics globals commingle harmlessly.
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

	grpcPort, err := freePort()
	if err != nil {
		return nil, a, err
	}
	a.grpcAddr = fmt.Sprintf("127.0.0.1:%d", grpcPort)

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

// wireMesh wires a full persistent-peer mesh: every node lists all others as
// nodeID@127.0.0.1:p2pPort (recipe #2 — testutil/network wires zero peers).
func wireMesh(nodes []*node) {
	for i, n := range nodes {
		var peers []string
		for j, peer := range nodes {
			if j == i {
				continue
			}
			peers = append(peers, fmt.Sprintf("%s@127.0.0.1:%s", peer.nodeID, peer.p2pPort))
		}
		n.tmCfg.P2P.PersistentPeers = strings.Join(peers, ",")
	}
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

// writeAppConfig writes a minimal per-node app.toml enabling gRPC on grpcAddr.
func writeAppConfig(path, grpcAddr string, opts Options) {
	appCfg := srvconfig.DefaultConfig()
	appCfg.Telemetry.Enabled = false
	appCfg.GRPC.Enable = true
	appCfg.GRPC.Address = grpcAddr
	srvconfig.WriteConfigFile(path, appCfg)
}

// freePort allocates a free loopback TCP port via server.FreeTCPAddr.
func freePort() (int, error) {
	_, portStr, err := server.FreeTCPAddr()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(portStr)
}
