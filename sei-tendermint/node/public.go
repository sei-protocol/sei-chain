// Package node provides a high level wrapper around tendermint services.
package node

import (
	"context"
	"fmt"

	abciclient "github.com/tendermint/tendermint/abci/client"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/service"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/types"
	"go.opentelemetry.io/otel/sdk/trace"
)

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

// NewDefault constructs a tendermint node service for use in go
// process that host their own process-local tendermint node. This is
// equivalent to running tendermint in it's own process communicating
// to an external ABCI application.
func NewDefault(
	ctx context.Context,
	conf *config.Config,
	logger log.Logger,
	restartCh chan struct{},
) (service.Service, error) {
	return newDefaultNode(ctx, conf, logger, restartCh)
}

// New constructs a tendermint node. The ClientCreator makes it
// possible to construct an ABCI application that runs in the same
// process as the tendermint node.  The final option is a pointer to a
// Genesis document: if the value is nil, the genesis document is read
// from the file specified in the config, and otherwise the node uses
// value of the final argument.
func New(
	ctx context.Context,
	conf *config.Config,
	logger log.Logger,
	restartCh chan struct{},
	cf abciclient.Client,
	gen *types.GenesisDoc,
	tracerProviderOptions []trace.TracerProviderOption,
	nodeMetrics *NodeMetrics,
	nodeOptions ...Option,
) (service.Service, error) {
	nodeKey, err := types.LoadOrGenNodeKey(conf.NodeKeyFile())
	if err != nil {
		return nil, fmt.Errorf("failed to load or gen node key %s: %w", conf.NodeKeyFile(), err)
	}

	var genProvider genesisDocProvider
	switch gen {
	case nil:
		genProvider = defaultGenesisDocProviderFunc(conf)
	default:
		genProvider = func() (*types.GenesisDoc, error) { return gen, nil }
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
			restartCh,
			pval,
			nodeKey,
			cf,
			genProvider,
			config.DefaultDBProvider,
			logger,
			tracerProviderOptions,
			nodeMetrics,
			nodeOptions...,
		)
	case config.ModeSeed:
		if resolveOptions(nodeOptions...).freezeHeight > 0 {
			return nil, fmt.Errorf("freeze height is not supported in seed mode")
		}
		return makeSeedNode(
			ctx,
			logger,
			conf,
			restartCh,
			config.DefaultDBProvider,
			nodeKey,
			genProvider,
			cf,
			nodeMetrics,
		)
	default:
		return nil, fmt.Errorf("%q is not a valid mode", conf.Mode)
	}
}
