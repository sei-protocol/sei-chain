// Package node provides a high level wrapper around tendermint services.
package node

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evmonlyapp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/privval"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/local"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/seilog"
	"go.opentelemetry.io/otel/sdk/trace"
)

var logger = seilog.NewLogger("tendermint", "node")

type options struct {
	freezeHeight uint64
}

// Option configures optional node behavior.
type Option func(*options)

// WithFreezeHeight stops block sync and consensus before executing height; 0 disables freezing.
func WithFreezeHeight(height uint64) Option {
	return func(opts *options) {
		opts.freezeHeight = height
	}
}

func resolveOptions(nodeOptions ...Option) options {
	var opts options
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
	app, storageManager, err := prepareApplication(conf, app)
	if err != nil {
		return nil, err
	}
	storageManagerTransferred := false
	defer func() {
		if err == nil || storageManagerTransferred {
			return
		}
		if manager, ok := storageManager.Get(); ok {
			err = errors.Join(err, manager.Close())
		}
	}()
	proxyApp := proxy.New(app)
	nodeKey, err := tmtypes.LoadOrGenNodeKey(conf.NodeKeyFile())
	if err != nil {
		return nil, fmt.Errorf("failed to load or gen node key %s: %w", conf.NodeKeyFile(), err)
	}

	var genProvider genesisDocProvider
	switch gen {
	case nil:
		genProvider = defaultGenesisDocProviderFunc(conf)
	default:
		genProvider = func() (*tmtypes.GenesisDoc, error) { return gen, nil }
	}

	switch conf.Mode {
	case config.ModeFull, config.ModeValidator:
		pval, err := privval.LoadOrGenFilePV(conf.PrivValidator.KeyFile(), conf.PrivValidator.StateFile())
		if err != nil {
			return nil, err
		}
		if conf.AutobahnConfigFile != "" && !storageManager.IsPresent() {
			manager, err := openAutobahnStorageManager(conf)
			if err != nil {
				return nil, fmt.Errorf("open Autobahn storage: %w", err)
			}
			storageManager = utils.Some(manager)
		}

		storageManagerTransferred = true
		return makeNode(
			ctx,
			conf,
			restartEvent,
			pval,
			nodeKey,
			proxyApp,
			genProvider,
			config.DefaultDBProvider,
			tracerProviderOptions,
			consensusPolicy,
			storageManager,
			nodeOptions...,
		)
	case config.ModeSeed:
		return makeSeedNode(
			conf,
			config.DefaultDBProvider,
			nodeKey,
			genProvider,
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
	if conf.MockApp && conf.AutobahnConfigFile == "" {
		return fmt.Errorf("mock-app requires autobahn-config-file")
	}
	if conf.EVMOnlyInMemory && conf.AutobahnConfigFile == "" {
		return fmt.Errorf("evm-only-in-memory requires autobahn-config-file")
	}
	return nil
}

func prepareApplication(
	conf *config.Config,
	app abci.Application,
) (abci.Application, utils.Option[*bootstrap.GigaStorageManager], error) {
	noStorage := utils.None[*bootstrap.GigaStorageManager]()
	if conf.EVMOnlyInMemory {
		fc, _, err := loadAutobahnCommittee(conf.AutobahnConfigFile)
		if err != nil {
			return nil, noStorage, fmt.Errorf("load EVM-only validator set: %w", err)
		}
		validators, err := evmOnlyValidatorUpdates(fc)
		if err != nil {
			return nil, noStorage, fmt.Errorf("load EVM-only validator set: %w", err)
		}
		blockStore, err := openAutobahnBlockStore(conf.RootDir, fc)
		if err != nil {
			return nil, noStorage, fmt.Errorf("open EVM-only block store: %w", err)
		}
		logger.Warn("Autobahn EVM-only in-memory execution enabled; state is ephemeral and unsafe for persistent networks")
		prepared, manager := evmonlyapp.NewEVMOnlyInMemoryApplication(
			config.AutobahnEVMOnlyInMemoryChainID,
			validators,
			blockStore,
		)
		return prepared, utils.Some(manager), nil
	}
	if conf.MockApp {
		return NewMockApp(app), noStorage, nil
	}
	if conf.FastCheckTx {
		return fastCheckTxApplication{Application: app}, noStorage, nil
	}
	return app, noStorage, nil
}

func evmOnlyValidatorUpdates(fc *config.AutobahnFileConfig) ([]abci.ValidatorUpdate, error) {
	validators := make([]abci.ValidatorUpdate, len(fc.Validators))
	for i, validator := range fc.Validators {
		key, err := ed25519.PublicKeyFromBytes(validator.ValidatorKey.Bytes())
		if err != nil {
			return nil, fmt.Errorf("validator %d public key: %w", i, err)
		}
		// BuildDataState assigns unit voting power to every configured Autobahn validator.
		validators[i] = abci.ValidatorUpdate{PubKey: crypto.PubKeyToProto(key), Power: 1}
	}
	return validators, nil
}
