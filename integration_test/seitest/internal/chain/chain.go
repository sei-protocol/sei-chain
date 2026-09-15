package chain

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
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

// encoding is the codec/tx-config bundle threaded through app.New and genesis
// assembly.
type encoding = appparams.EncodingConfig

// chainIDPrefix prefixes every engine-generated chain id.
const chainIDPrefix = "seitest"

// GenesisAccount is a funded non-validator signing account created at genesis.
type GenesisAccount struct {
	// Name is the key name in the chain-wide keyring.
	Name string
	// Coins is the account's genesis balance. Empty creates the account with
	// nothing in it.
	Coins sdk.Coins
}

// Config configures a Start. The zero value brings up a single validator with a
// fresh chain id in an engine-owned temp dir.
type Config struct {
	// Validators is the validator count. Valid values are 1 and >= 3; 2 is
	// rejected because it deadlocks in block-sync (see validateTopology). 0 means
	// 1.
	Validators int

	// ChainID is the genesis chain id; "" generates a fresh id per Start so a run
	// never inherits a prior run's persisted genesis.
	ChainID string

	// HomeDir is the parent dir for per-validator homes; "" creates a temp dir
	// the engine owns and removes at Close. A caller-supplied dir is left alone.
	HomeDir string

	// GenesisAccounts are funded non-validator signing accounts.
	GenesisAccounts []GenesisAccount

	// Timeouts are the consensus timeouts; nil uses the sei-tendermint defaults,
	// which are already fast (50ms commit, 50ms vote, 1s propose).
	Timeouts *tmtypes.TimeoutParams

	// ServeTendermintRPC binds an HTTP CometBFT RPC listener per validator. The
	// default, false, binds none: the in-process client needs no listener, and one
	// P2P port per validator is the whole port footprint.
	ServeTendermintRPC bool

	// LogWriter receives the app's commit-store trace; nil discards it. Node and
	// module logging is governed process-wide by seilog, not by this field.
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

// freshChainID returns a chain id unique to this Start, falling back to a
// nanosecond timestamp if crypto/rand is unavailable.
func freshChainID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%d", chainIDPrefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%x", chainIDPrefix, b[:])
}

// Chain is a handle to a running in-process network. It owns keys, ports,
// config, genesis, consensus, and lifecycle; it builds no messages and knows
// nothing about testing.T.
//
// A Chain is not safe for concurrent use beyond Close, which may be called from
// any goroutine and any number of times.
type Chain struct {
	cfg             Config
	homeDir         string
	ownHome         bool // true => Close removes homeDir
	keyring         keyring.Keyring
	consensusParams *tmtypes.ConsensusParams
	validators      []*Validator

	// cancelEngine stops everything the engine started. It is distinct from the
	// ctx passed to Start, which bounds bring-up only.
	cancelEngine context.CancelFunc

	closeOnce sync.Once
	closeErr  error
}

// Validators is the validator count.
func (c *Chain) Validators() int { return len(c.validators) }

// Validator returns the i-th validator, 0-based. It panics on an out-of-range
// index, which is a programming error rather than a runtime condition.
func (c *Chain) Validator(i int) *Validator { return c.validators[i] }

// ChainID is the genesis chain id the network is running under.
func (c *Chain) ChainID() string { return c.cfg.ChainID }

// HomeDir is the parent dir holding every validator's home.
func (c *Chain) HomeDir() string { return c.homeDir }

// gentxsDir is where the provisioning pass writes gentxs for collectGentxs.
func (c *Chain) gentxsDir() string { return filepath.Join(c.homeDir, "gentxs") }

// Start brings up cfg.Validators validators and returns once every one is
// constructed and started — not once consensus is live. Call WaitReady for that.
//
// ctx bounds bring-up only. The network itself runs on an engine-owned context
// that Close cancels, so a caller whose ctx expires after Start returns keeps a
// live chain rather than a silently dead one.
//
// On any error mid-bring-up, Start tears down whatever came up through the same
// Close the caller would have called, so a failed Start leaks no store, no
// goroutine, and no temp dir. On success the caller owns Close.
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
	// a bring-up deadline, and a node whose context dies when that deadline
	// passes is a network that stops without anyone asking it to.
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
		if retErr != nil {
			retErr = errors.Join(retErr, c.Close())
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

// consensusParamsFor builds the genesis consensus params, overriding the
// sei-tendermint defaults only where the caller asked.
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

	// An in-memory tendermint DB with the SeiDB stores on the validator's home
	// dir, no skipped upgrade heights, and custom EVM precompiles off — the
	// production default for a chain that serves no EVM. The bare true and 1 are
	// app.New parameters it ignores.
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
// The set is empty for N>=2 so every node derives its valset from its own
// InitChain response; a pre-populated set fails consensus replay. N=1 is the
// exception: a solo validator must skip block-sync, which happens only when
// sei-tendermint's onlyValidatorIsUs sees a one-entry valset carrying our
// consensus key — and it reads that from genesis before InitChain runs, so an
// empty set there leaves a 0-peer node hung in block-sync forever.
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

// Close tears the network down and is idempotent: a second call returns the
// first call's result without touching anything. Errors from every step are
// joined rather than short-circuited, so one validator failing to close does not
// hide the rest or skip removing the home dir.
//
// It is also the teardown Start uses on a failed bring-up, so the partial-start
// path is the same code the success path exercises.
func (c *Chain) Close() error {
	c.closeOnce.Do(func() {
		c.cancelEngine()
		errs := make([]error, 0, len(c.validators)+1)
		for _, v := range c.validators {
			errs = append(errs, closeValidator(v))
		}
		if c.ownHome && c.homeDir != "" {
			errs = append(errs, os.RemoveAll(c.homeDir))
		}
		c.closeErr = errors.Join(errs...)
	})
	return c.closeErr
}

// closeValidator stops one validator's consensus before closing its stores, and
// is a no-op for a field a partial bring-up never populated.
//
// Consensus stops first because BaseApp.Close takes the commit lock, so closing
// stores under a running node waits on — or races — an in-flight commit. Stop is
// what makes that ordering real: sei-tendermint's BaseService.Stop blocks until
// the service is fully stopped, so app.Close cannot run against a node that is
// only part-way down. It is called unconditionally rather than behind
// IsRunning, which reports false as soon as a shutdown this Close did not start
// is in flight — and skipping the join there is exactly the race the ordering
// exists to prevent. Stop and Wait are both no-ops on a node that never started.
//
// Each field is cleared as it is consumed because BaseApp.Close is not itself
// idempotent: closing an already closed store errors.
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
