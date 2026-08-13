// Package node provides a high level wrapper around tendermint services.
package node

import (
	"context"
	"fmt"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/service"
	"github.com/sei-protocol/sei-chain/sei-tendermint/privval"
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

// New constructs a tendermint node. The provided app runs in the same
// process as the tendermint node and will be wrapped in a local ABCI client
// inside this function. The final option is a pointer to a Genesis document:
// if the value is nil, the genesis document is read from the file specified
// in the config, and otherwise the node uses value of the final argument.
func New(
	ctx context.Context,
	conf *config.Config,
	restartEvent func(),
	app abci.Application,
	gen *tmtypes.GenesisDoc,
	tracerProviderOptions []trace.TracerProviderOption,
	nodeMetrics *NodeMetrics,
	nodeOptions ...Option,
) (service.Service, error) {
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
			app,
			genProvider,
			config.DefaultDBProvider,
			tracerProviderOptions,
			nodeMetrics,
			nodeOptions...,
		)
	case config.ModeSeed:
		if resolveOptions(nodeOptions...).freezeHeight > 0 {
			return nil, fmt.Errorf("freeze height is not supported in seed mode")
		}
		return makeSeedNode(
			conf,
			config.DefaultDBProvider,
			nodeKey,
			genProvider,
			nodeMetrics,
		)
	default:
		return nil, fmt.Errorf("%q is not a valid mode", conf.Mode)
	}
}
