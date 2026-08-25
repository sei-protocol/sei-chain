// Package node provides a high level wrapper around tendermint services.
package node

import (
	"context"
	"fmt"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
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
) (local.NodeService, error) {
	if err := validateNodeSetupConfig(conf); err != nil {
		return nil, err
	}
	opts := resolveOptions(nodeOptions...)
	if err := validateFreezeMode(conf.Mode, opts.freezeHeight); err != nil {
		return nil, err
	}
	app = prepareApplication(conf, app)
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
	return nil
}

func prepareApplication(conf *config.Config, app abci.Application) abci.Application {
	if conf.MockApp {
		return NewMockApp(app)
	}
	if conf.FastCheckTx {
		return fastCheckTxApplication{Application: app}
	}
	return app
}
