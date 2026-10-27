package chain

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	dbm "github.com/tendermint/tm-db"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/app"
	appparams "github.com/sei-protocol/sei-chain/app/params"
	cryptocodec "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	tmnode "github.com/sei-protocol/sei-chain/sei-tendermint/node"
	rpclocal "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/local"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
)

type encoding = appparams.EncodingConfig

// chainIDPrefix prefixes every engine-generated chain id.
const chainIDPrefix = "seitest"

// GenesisAccount is a funded non-validator signing account created at genesis.
type GenesisAccount struct {
	// Name is the key name in the chain-wide keyring.
	Name string
	// Coins is the account's genesis balance
	Coins sdk.Coins
}

type Config struct {
	// Validators is the validator count. Valid values are 1 and >= 3
	Validators int

	// ChainID is the genesis chain id
	ChainID string

	// HomeDir is the parent dir for per-validator homes; "" creates a temp dir
	// the engine owns and removes at Close. A caller-supplied dir is left alone.
	HomeDir string

	// GenesisAccounts are funded non-validator signing accounts.
	GenesisAccounts []GenesisAccount

	// Timeouts are the consensus timeouts; nil uses the sei-tendermint defaults
	Timeouts *tmtypes.TimeoutParams

	// ServeTendermintRPC binds an HTTP CometBFT RPC listener per validator.
	ServeTendermintRPC bool

	LogWriter io.Writer
}

// withDefaults resolves the zero values Config documents.
func (cfg Config) withDefaults() Config {
	if cfg.Validators == 0 {
		cfg.Validators = 1
	}
	if cfg.ChainID == "" {
		cfg.ChainID = freshChainID()
	}
	if cfg.LogWriter == nil {
		cfg.LogWriter = io.Discard
	}
	return cfg
}

func freshChainID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%d", chainIDPrefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%x", chainIDPrefix, b[:])
}

// Chain is a handle to a running in-process network. It owns keys, ports,
// config, genesis, consensus, and lifecycle
type Chain struct {
	cfg             Config
	homeDir         string
	ownHome         bool
	keyring         keyring.Keyring
	consensusParams *tmtypes.ConsensusParams
	validators      []*Validator

	cancelEngine context.CancelFunc

	closeErr error
}

// Validators is the validator count.
func (c *Chain) Validators() int { return len(c.validators) }

// Validator returns the i-th validator.
func (c *Chain) Validator(i int) *Validator { return c.validators[i] }

// ChainID is the genesis chain id the network is running under.
func (c *Chain) ChainID() string { return c.cfg.ChainID }

// HomeDir is the parent dir holding every validator's home.
func (c *Chain) HomeDir() string { return c.homeDir }

// gentxsDir is where the provisioning pass writes gentxs for collectGentxs.
func (c *Chain) gentxsDir() string { return filepath.Join(c.homeDir, "gentxs") }

// Start brings up cfg.Validators validators and returns once every one is
// constructed and started — not once consensus is live. Call WaitReady for that.
func Start(ctx context.Context, cfg Config) (_ *Chain, retErr error) {
	cfg = cfg.withDefaults()
	if err := validateTopology(cfg.Validators); err != nil {
		return nil, err
	}

	homeDir, ownHome, err := resolveHome(cfg.HomeDir)
	if err != nil {
		return nil, err
	}

	// The engine context deliberately does not inherit ctx's cancellation: ctx is
	// a bring-up deadline.
	engineCtx, cancelEngine := context.WithCancel(context.WithoutCancel(ctx))

	c := &Chain{
		cfg:             cfg,
		homeDir:         homeDir,
		ownHome:         ownHome,
		keyring:         keyring.NewInMemory(),
		consensusParams: consensusParamsFor(cfg.Timeouts),
		cancelEngine:    cancelEngine,
	}
	// Past this point every failure path tears the chain down, so the caller
	// never holds — or has to clean up after — a half-built network.
	defer func() {
		err := recover()
		if retErr != nil {
			retErr = errors.Join(retErr, c.Close())
		}
		if err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("panic: %v", err))
		}
	}()

	enc := app.MakeEncodingConfig()
	gb := &genesisBuilder{
		codec:     enc.Marshaler,
		txConfig:  enc.TxConfig,
		chainID:   cfg.ChainID,
		bondDenom: sdk.DefaultBondDenom,
	}

	if err := c.provisionValidators(enc, gb); err != nil {
		return nil, err
	}
	if err := c.provisionGenesisAccounts(gb); err != nil {
		return nil, err
	}

	if err := c.writeBaseGenesis(gb, enc); err != nil {
		return nil, err
	}
	if err := gb.collectGentxs(c.validators, c.gentxsDir()); err != nil {
		return nil, fmt.Errorf("collect gentxs: %w", err)
	}
	if err := assertPeerMesh(c.validators); err != nil {
		return nil, err
	}
	if err := assertGenesisConsensusParams(c.validators, c.consensusParams); err != nil {
		return nil, err
	}

	for _, v := range c.validators {
		// Bring-up is the long pole, so ctx is checked between validators rather
		// than only at the end — a caller's deadline should stop the work it can
		// still stop.
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("bring-up cancelled before starting %s: %w", v.moniker, err)
		}
		if err := c.startValidator(ctx, engineCtx, v, enc); err != nil {
			return nil, fmt.Errorf("start %s: %w", v.moniker, err)
		}
	}
	return c, nil
}

// writeBaseGenesis writes the pre-gentx genesis doc to every validator, over the
// module defaults plus the accounts provisioning accumulated.
func (c *Chain) writeBaseGenesis(gb *genesisBuilder, enc encoding) error {
	genFiles := make([]string, len(c.validators))
	for i, v := range c.validators {
		genFiles[i] = v.tmCfg.GenesisFile()
	}
	if err := gb.writeBaseGenesis(app.ModuleBasics.DefaultGenesis(enc.Marshaler), c.consensusParams, genFiles); err != nil {
		return fmt.Errorf("write base genesis: %w", err)
	}
	return nil
}

func consensusParamsFor(timeouts *tmtypes.TimeoutParams) *tmtypes.ConsensusParams {
	params := tmtypes.DefaultConsensusParams()
	if timeouts != nil {
		params.Timeout = *timeouts
	}
	return params
}

// resolveHome returns the parent dir for validator homes and whether the engine
// owns it, and so must remove it at Close.
func resolveHome(dir string) (string, bool, error) {
	if dir != "" {
		return dir, false, nil
	}
	tmp, err := os.MkdirTemp("", "seitest-")
	if err != nil {
		return "", false, fmt.Errorf("create home dir: %w", err)
	}
	return tmp, true, nil
}

// startValidator is the choke point: every invariant guard runs here,
// immediately before the app and node are built, so a refactor that rebuilds a
// config or an options set elsewhere still has to satisfy them.
//
// The two contexts are two different lifetimes and are not interchangeable.
// bringUp is the caller's deadline for construction; engine outlives Start and
// is what the node runs on until Close cancels it.
func (c *Chain) startValidator(bringUp, engine context.Context, v *Validator, enc encoding) error {
	if err := assertConfigInvariants(v.tmCfg, v.moniker, len(c.validators)); err != nil {
		return err
	}
	appOpts := appOptions{chainID: c.cfg.ChainID}
	if err := assertEVMServingOff(appOpts); err != nil {
		return err
	}

	genDoc, err := c.genesisDocFor(v)
	if err != nil {
		return err
	}

	// SeiDB stores live on the validator's home dir; no skipped upgrade heights;
	// EVM precompiles off (production default for a chain serving no EVM). The
	// bare true and 1 are app.New parameters it ignores.
	v.app = app.New(
		dbm.NewMemDB(),
		c.cfg.LogWriter,
		true,
		map[int64]bool{},
		v.home,
		1,
		false,
		v.tmCfg,
		enc,
		wasm.EnableAllProposals,
		appOpts,
		app.EmptyWasmOpts,
		nil,
	)

	// The app is the most expensive thing bring-up builds and the node is the
	// point of no return, so a caller who has already given up is answered here
	// rather than handed a chain it no longer wants. Close covers the app this
	// leaves behind.
	if err := bringUp.Err(); err != nil {
		return fmt.Errorf("bring-up cancelled after building the app: %w", err)
	}

	tmNode, err := tmnode.New(
		engine, v.tmCfg, func() {}, v.app, genDoc,
		[]trace.TracerProviderOption{},
		tmtypes.DefaultConsensusPolicy(),
	)
	if err != nil {
		return fmt.Errorf("node.New: %w", err)
	}
	v.tmNode = tmNode
	if err := tmNode.Start(engine); err != nil {
		return fmt.Errorf("node.Start: %w", err)
	}

	lc, err := rpclocal.New(tmNode)
	if err != nil {
		return fmt.Errorf("local client: %w", err)
	}
	v.rpc = lc
	v.clientCx = v.clientCx.WithClient(lc)
	// With EVM serving off this registers only the tx and tendermint gRPC
	// services on the in-process query router — no listener, no worker pool.
	v.app.RegisterLocalServices(lc, v.clientCx.TxConfig)
	return nil
}

// genesisDocFor loads v's genesis doc and sets the validator set CometBFT should
// start from.
//
// Empty for N>=2: each node then derives its valset from its own InitChain
// response, since a pre-populated set fails consensus replay. N=1 is the
// exception — sei-tendermint's onlyValidatorIsUs skips block-sync only when it
// sees a one-entry valset carrying our consensus key in genesis, read before
// InitChain runs, so an empty set there hangs a 0-peer node forever.
func (c *Chain) genesisDocFor(v *Validator) (*tmtypes.GenesisDoc, error) {
	genDoc, err := tmtypes.GenesisDocFromFile(v.tmCfg.GenesisFile())
	if err != nil {
		return nil, err
	}
	genDoc.Validators = nil
	if len(c.validators) == 1 {
		tmPub, err := cryptocodec.ToTmPubKeyInterface(v.pubKey)
		if err != nil {
			return nil, fmt.Errorf("convert consensus pubkey for %s: %w", v.moniker, err)
		}
		genDoc.Validators = []tmtypes.GenesisValidator{
			{PubKey: tmPub, Address: tmPub.Address(), Name: v.moniker, Power: 100},
		}
	}
	return genDoc, nil
}

func (c *Chain) Close() error {
	c.cancelEngine()
	errs := make([]error, 0, len(c.validators)+1)
	for _, v := range c.validators {
		errs = append(errs, closeValidator(v))
	}
	if c.ownHome && c.homeDir != "" {
		errs = append(errs, os.RemoveAll(c.homeDir))
	}
	c.closeErr = errors.Join(errs...)
	return c.closeErr
}

func closeValidator(v *Validator) error {
	var errs []error
	if v.tmNode != nil {
		v.tmNode.Stop()
		v.tmNode.Wait()
		v.tmNode = nil
	}
	if v.app != nil {
		if err := v.app.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close %s app: %w", v.moniker, err))
		}
		v.app = nil
	}
	v.rpc = nil
	return errors.Join(errs...)
}
