// Package node provides a high level wrapper around tendermint services.
package node

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"runtime/debug"

	"github.com/sei-protocol/seilog"
	"go.opentelemetry.io/otel/sdk/trace"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evmonlyapp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/local"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var logger = seilog.NewLogger("tendermint", "node")

type options struct {
	freezeHeight uint64
	giga         gigaconfig.Config
}

var errAutobahnSeed = errors.New("autobahn is not supported in seed mode")

// Option configures optional node behavior.
type Option func(*options)

// WithFreezeHeight stops block sync and consensus before executing height; 0 disables freezing.
func WithFreezeHeight(height uint64) Option {
	return func(opts *options) {
		opts.freezeHeight = height
	}
}

// WithGigaConfig sets the [giga] app.toml section a Giga node opens its storage and runs its
// EVM-only executor with. Without it the node runs gigaconfig.DefaultConfig.
func WithGigaConfig(cfg gigaconfig.Config) Option {
	return func(opts *options) {
		opts.giga = cfg
	}
}

func resolveOptions(nodeOptions ...Option) options {
	opts := options{giga: gigaconfig.DefaultConfig}
	for _, apply := range nodeOptions {
		apply(&opts)
	}
	return opts
}

// New constructs a Tendermint node around an in-process ABCI application.
// A non-nil genesis document overrides the file selected by the node config.
func New(
	ctx context.Context,
	conf *config.Config,
	restartEvent func(),
	app abci.Application,
	gen *tmtypes.GenesisDoc,
	tracerProviderOptions []trace.TracerProviderOption,
	consensusPolicy tmtypes.ConsensusPolicy,
	nodeOptions ...Option,
) (_ local.NodeService, err error) {
	if err := validateNodeSetupConfig(conf); err != nil {
		return nil, err
	}
	opts := resolveOptions(nodeOptions...)
	if err := validateFreezeMode(conf.Mode, opts.freezeHeight); err != nil {
		return nil, err
	}
	if err := opts.giga.Validate(); err != nil {
		return nil, fmt.Errorf("giga config: %w", err)
	}
	if conf.AutobahnConfigFile != "" {
		applyExecutionRuntime(opts.giga.Execution)
	}
	genDoc, err := genesisDocument(conf, gen)
	if err != nil {
		return nil, err
	}
	nodeKey, err := tmtypes.LoadOrGenNodeKey(conf.NodeKeyFile())
	if err != nil {
		return nil, fmt.Errorf("failed to load or gen node key %s: %w", conf.NodeKeyFile(), err)
	}

	switch conf.Mode {
	case config.ModeFull, config.ModeValidator:
		return makeNode(
			ctx,
			conf,
			restartEvent,
			nodeKey,
			app,
			genDoc,
			config.DefaultDBProvider,
			tracerProviderOptions,
			consensusPolicy,
			nodeOptions...,
		)
	case config.ModeSeed:
		return makeSeedNode(
			conf,
			config.DefaultDBProvider,
			nodeKey,
			genDoc,
		)
	default:
		return nil, fmt.Errorf("%q is not a valid mode", conf.Mode)
	}
}

func validateFreezeMode(mode string, freezeHeight uint64) error {
	if freezeHeight == 0 || mode == config.ModeFull {
		return nil
	}
	switch mode {
	case config.ModeValidator, config.ModeSeed:
		return fmt.Errorf("freeze height is not supported in %s mode", mode)
	default:
		return nil
	}
}

func validateNodeSetupConfig(conf *config.Config) error {
	if conf.AutobahnConfigFile != "" && conf.Mode == config.ModeSeed {
		return errAutobahnSeed
	}
	return nil
}

// genesisDocument returns the genesis document the node starts from. A nil
// document is loaded from the config's genesis file. The result is validated.
func genesisDocument(conf *config.Config, gen *tmtypes.GenesisDoc) (*tmtypes.GenesisDoc, error) {
	if gen == nil {
		loaded, err := tmtypes.GenesisDocFromFile(conf.GenesisFile())
		if err != nil {
			return nil, fmt.Errorf("load genesis document: %w", err)
		}
		gen = loaded
	}
	if err := gen.ValidateAndComplete(); err != nil {
		return nil, fmt.Errorf("error in genesis doc: %w", err)
	}
	return gen, nil
}

// makeEVMApplication opens Autobahn storage and constructs its EVM application.
// The caller owns the returned storage after construction succeeds.
func makeEVMApplication(
	ctx context.Context,
	conf *config.Config,
	giga gigaconfig.Config,
	genDoc *tmtypes.GenesisDoc,
) (_ proxy.EVMApplication, _ *bootstrap.GigaStorageManager, err error) {
	committee, _, err := loadAutobahnCommittee(conf.AutobahnConfigFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load Autobahn committee: %w", err)
	}
	storage, err := openAutobahnStorageManager(ctx, conf.RootDir, conf.Mode, committee, giga.Storage, genDoc)
	if err != nil {
		return nil, nil, fmt.Errorf("open Autobahn storage: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, storage.Close())
		}
	}()
	validators, err := evmOnlyValidatorUpdates(committee)
	if err != nil {
		return nil, nil, fmt.Errorf("load EVM-only validator set: %w", err)
	}
	customPrecompiles, err := evmOnlyCustomPrecompiles(committee, validators, giga.Execution.UnsafeSkipUpgrades)
	if err != nil {
		return nil, nil, fmt.Errorf("load EVM-only governance: %w", err)
	}
	logger.Info("Autobahn EVM-only execution enabled with disk-backed Giga storage")
	app, err := evmonlyapp.NewEVMOnlyApplication(
		config.AutobahnEVMOnlyChainID,
		validators,
		storage,
		giga.Execution,
		customPrecompiles,
		utils.Some(conf.RootDir),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("restore EVM-only application: %w", err)
	}
	if conf.MockApp {
		mockApp := NewMockApp(app)
		mockApp.hashes = utils.Some(storage.AppHashBuilder())
		return mockApp, storage, nil
	}
	return app, storage, nil
}

func evmOnlyValidatorUpdates(fc *config.AutobahnFileConfig) ([]proxy.ValidatorUpdate, error) {
	validators := make([]proxy.ValidatorUpdate, len(fc.Validators))
	for i, validator := range fc.Validators {
		// BuildDataState assigns unit voting power to every configured Autobahn validator.
		validators[i] = proxy.ValidatorUpdate{PubKey: validator.ValidatorKey, Power: 1}
	}
	return validators, nil
}

// evmOnlyCustomPrecompiles returns the gov precompile registry when the
// committee sets evm_governance, with each validator's evm_voter voting at that
// validator's power, skipping upgrade plans at skipUpgradeHeights.
func evmOnlyCustomPrecompiles(fc *config.AutobahnFileConfig, validators []proxy.ValidatorUpdate, skipUpgradeHeights []uint64) (utils.Option[precompiles.Registry], error) {
	governance, ok := fc.EVMGovernance.Get()
	if !ok {
		return utils.None[precompiles.Registry](), nil
	}
	electorate := make(gov.GenesisElectorate, len(fc.Validators))
	for i, validator := range fc.Validators {
		voter, ok := validator.EVMVoter.Get()
		if !ok {
			return utils.None[precompiles.Registry](), fmt.Errorf("validator %d is missing evm_voter", i)
		}
		electorate[voter] = big.NewInt(validators[i].Power)
	}
	params, err := governance.Params.Gov()
	if err != nil {
		return utils.None[precompiles.Registry](), err
	}
	return utils.Some(evmonlyapp.NewGovRegistry(params, governance.StartingProposalID, electorate, evmonlyapp.GovUpgrades{
		Applied:     governance.AppliedUpgrades,
		SkipHeights: skipUpgradeHeights,
	})), nil
}

// applyExecutionRuntime applies GCPercent when set, and caps the heap at 80% of the container
// memory limit when one is present. Call only for Autobahn/Giga nodes.
func applyExecutionRuntime(cfg gigaconfig.ExecutionConfig) {
	if cfg.GCPercent <= 0 {
		return
	}
	debug.SetGCPercent(cfg.GCPercent)
	limit := containerMemoryLimit() / 10 * 8
	// A limit the operator set through GOMEMLIMIT is only ever lowered, never raised.
	if limit <= 0 || limit >= debug.SetMemoryLimit(-1) {
		logger.Info("raised the Go GC target", "gc_percent", cfg.GCPercent)
		return
	}
	debug.SetMemoryLimit(limit)
	logger.Info("raised the Go GC target", "gc_percent", cfg.GCPercent, "memory_limit", limit)
}
