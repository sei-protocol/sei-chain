package chain

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	rpclocal "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/local"
)

// Validator is a handle to one in-process validator: its identity, its home dir,
// and — once Start has run — its app, node, and in-process RPC client.
type Validator struct {
	moniker string
	nodeID  string
	pubKey  cryptotypes.PubKey
	addr    sdk.AccAddress
	home    string

	tmCfg    *config.Config
	clientCx client.Context

	p2pHost string
	p2pPort string

	app    *app.App
	tmNode rpclocal.NodeService
	rpc    *rpclocal.Local
}

// Moniker is the validator's name (node0, node1, ...).
func (v *Validator) Moniker() string { return v.moniker }

// Home is the validator's on-disk home dir, holding its config/ and data/.
func (v *Validator) Home() string { return v.home }

// OperatorAddr is the account address of the validator's operator key.
func (v *Validator) OperatorAddr() sdk.AccAddress { return v.addr }

// ValAddr is the validator's operator address in validator-operator form.
func (v *Validator) ValAddr() sdk.ValAddress { return sdk.ValAddress(v.addr) }

// LocalClient is the in-process CometBFT RPC client. It is nil until Start has
// started this validator, and it binds no listener — every call is a direct
// function call into the node's RPC environment.
func (v *Validator) LocalClient() *rpclocal.Local { return v.rpc }

// ClientContext is the codec/keyring/tx-config bundle for building txs and
// queries against this validator. Once started it carries LocalClient.
func (v *Validator) ClientContext() client.Context { return v.clientCx }

// Keyring is the chain-wide in-memory keyring. Every validator shares it, so a
// key added through any validator signs through all of them.
func (v *Validator) Keyring() keyring.Keyring { return v.clientCx.Keyring }

// App is the running sei app. It is the read-oriented escape hatch for state
// the engine's own surface does not expose; driving it past the ABCI path
// bypasses consensus.
func (v *Validator) App() *app.App { return v.app }

// provisionValidators runs the first bring-up pass: per-validator home dir,
// consensus and node keys, tendermint config, one P2P port, and the operator key
// funded into genesis with its self-delegation gentx.
//
// Every validator shares one in-memory keyring so an account is chain-wide
// identity and the validator merely selects which node answers.
func (c *Chain) provisionValidators(enc encoding, gb *genesisBuilder) error {
	algo, err := signingAlgo(c.keyring)
	if err != nil {
		return err
	}
	for i := 0; i < c.cfg.Validators; i++ {
		moniker := fmt.Sprintf("node%d", i)
		home := filepath.Join(c.homeDir, moniker)
		if err := os.MkdirAll(filepath.Join(home, "config"), 0o750); err != nil {
			return err
		}

		tmCfg, p2pPort, err := newValidatorConfig(home, moniker, c.cfg.ServeTendermintRPC)
		if err != nil {
			return fmt.Errorf("config for %s: %w", moniker, err)
		}

		nodeID, pubKey, err := genutil.InitializeNodeValidatorFiles(tmCfg)
		if err != nil {
			return fmt.Errorf("init validator files for %s: %w", moniker, err)
		}

		addr, err := gb.fundValidator(c.keyring, moniker, pubKey, algo, loopbackHost, p2pPort, nodeID, c.gentxsDir())
		if err != nil {
			return err
		}

		c.validators = append(c.validators, &Validator{
			moniker: moniker, nodeID: nodeID, pubKey: pubKey, addr: addr, home: home,
			tmCfg: tmCfg, clientCx: newClientContext(enc, c.keyring, tmCfg.RootDir, c.cfg.ChainID),
			p2pHost: loopbackHost, p2pPort: p2pPort,
		})
	}
	return nil
}

// newClientContext builds the codec/keyring/tx-config bundle for one validator.
// The in-process RPC client is attached later by startValidator, once the node
// exists.
func newClientContext(enc encoding, kb keyring.Keyring, home, chainID string) client.Context {
	return client.Context{}.
		WithKeyring(kb).WithKeyringDir(home).WithHomeDir(home).
		WithChainID(chainID).
		WithInterfaceRegistry(enc.InterfaceRegistry).
		WithCodec(enc.Marshaler).WithLegacyAmino(enc.Amino).
		WithTxConfig(enc.TxConfig).
		WithAccountRetriever(authtypes.AccountRetriever{})
}

// loopbackHost is the only address the engine binds or dials. Scoping every
// listener to it is the loopback bind scope invariant (see doc.go).
const loopbackHost = "127.0.0.1"

// newValidatorConfig is the sole *config.Config constructor in the engine, and
// every bring-up invariant that lives in the tendermint config is established
// here: metrics off, loopback bind scope, the loopback conn-tracker ceiling, and
// no RPC listener unless the caller asked for one. assertConfigInvariants
// re-checks them at startValidator, so a future refactor that clones or rebuilds
// a config still has to satisfy them.
//
// It returns the config and the validator's P2P port, the one port a validator
// binds.
func newValidatorConfig(home, moniker string, serveRPC bool) (*config.Config, string, error) {
	cfg := config.DefaultConfig()
	cfg.Mode = config.ModeValidator
	cfg.Moniker = moniker
	cfg.SetRoot(home)
	cfg.TxIndex = config.TestTxIndexConfig()

	// metrics-off: the Prometheus listener binds a fixed port (:26660), so a
	// second validator in the same process collides with the first.
	cfg.Instrumentation.Prometheus = false

	// loopback conn-tracker ceiling: loopback collapses every peer onto one IP,
	// so the router's IP-keyed conn-tracker counts the whole startup burst
	// against a single key and rejects peers once the per-IP cap trips.
	// AllowDuplicateIP is a peer-manager flag and does not touch the conn-tracker.
	cfg.P2P.MaxIncomingConnectionAttempts = 10000
	cfg.P2P.AllowDuplicateIP = true

	// no-listener bring-up: sei-tendermint gates its whole RPC service on a
	// non-empty RPC.ListenAddress (node/node.go), while rpcEnv is built
	// unconditionally — so rpclocal.New works with nothing bound. Serving it is
	// the recorded fallback, costing one port per validator.
	cfg.RPC.ListenAddress = ""
	if serveRPC {
		port, err := freePort()
		if err != nil {
			return nil, "", err
		}
		cfg.RPC.ListenAddress = fmt.Sprintf("tcp://%s:%d", loopbackHost, port)
	}

	p2pPort, err := freePort()
	if err != nil {
		return nil, "", err
	}
	cfg.P2P.ListenAddress = fmt.Sprintf("tcp://%s:%d", loopbackHost, p2pPort)

	return cfg, strconv.Itoa(p2pPort), nil
}

var (
	allocatedPortsMu sync.Mutex
	allocatedPorts   = map[int]struct{}{}
)

// freePort returns a TCP port free on the IPv4 loopback that this process has
// not already handed out. Two hazards make a bare probe-close-return flaky: on a
// dual-stack host "localhost" can resolve to ::1, verifying a port free on IPv6
// while it stays bound on IPv4, so the probe names 127.0.0.1 explicitly; and two
// probes can return the same port intra-process, so the allocated set rejects a
// repeat. A bind-time race with an unrelated process is the residual TOCTOU.
func freePort() (int, error) {
	allocatedPortsMu.Lock()
	defer allocatedPortsMu.Unlock()
	for attempt := 0; attempt < 100; attempt++ {
		l, err := net.Listen("tcp", net.JoinHostPort(loopbackHost, "0"))
		if err != nil {
			return 0, fmt.Errorf("allocate loopback port: %w", err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		if _, taken := allocatedPorts[port]; taken {
			continue
		}
		allocatedPorts[port] = struct{}{}
		return port, nil
	}
	return 0, fmt.Errorf("no free loopback port after 100 attempts")
}
