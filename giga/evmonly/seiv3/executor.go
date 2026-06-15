package seiv3

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

// Executor is the placeholder for the sei-v3-derived EVM-only executor.
type Executor struct {
	cfg Config
}

func NewExecutor(cfg Config) *Executor {
	return &Executor{cfg: cfg.WithDefaults()}
}

func (e *Executor) Config() Config {
	return e.cfg
}

func (e *Executor) ExecuteBlock(ctx context.Context, req evmonly.BlockRequest) (*evmonly.BlockResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if len(req.Txs) == 0 {
		return &evmonly.BlockResult{}, nil
	}

	return nil, fmt.Errorf(
		"%w: port sei-v3 parser, EVMC host context, OCC scheduler, EVM-native stores, and receipt pipeline",
		evmonly.ErrNotImplemented,
	)
}
