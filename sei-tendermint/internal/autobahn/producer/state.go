package producer

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"golang.org/x/time/rate"
)

// Config is the config of the block scope.
type Config struct {
	MaxGasPerBlock   uint64
	MaxTxsPerBlock   uint64
	MaxTxsPerSecond  utils.Option[uint64]
	MempoolSize      uint64
	BlockInterval    time.Duration
	AllowEmptyBlocks bool
}

// minTxGas is the minimum gas cost of an evm tx.
const minTxGas = 21000

func (c *Config) maxTxsPerBlock() uint64 {
	return min(c.MaxTxsPerBlock, c.MaxGasPerBlock/minTxGas)
}

// MaxGasPerBlockI64 returns MaxGasPerBlock clamped to the int64 range.
// Config validation only enforces > 0 (sei-tendermint/config/autobahn.go),
// so a misconfigured chain with a value above math.MaxInt64 can't silently
// overflow when consumed by APIs that take int64 (the mempool's ReapLimits,
// the RPC layer's ConsensusParamUpdates.Block.MaxGas). Centralizing the
// clamp here means callers pick this up by name instead of repeating
// utils.Clamp[int64] at every site, and any future change to the clamp
// rule (or the underlying field type) lives in one place.
func (c *Config) MaxGasPerBlockI64() int64 {
	return utils.Clamp[int64](c.MaxGasPerBlock)
}

// State is the block producer state.
type State struct {
	cfg       *Config
	txMempool *mempool.TxMempool
	// consensus state to which published blocks will be reported.
	consensus *consensus.State
}

// NewState constructs a new block producer state.
// Returns an error if the current node is NOT a producer.
func NewState(cfg *Config, txMempool *mempool.TxMempool, consensus *consensus.State) *State {
	return &State{
		cfg:       cfg,
		txMempool: txMempool,
		consensus: consensus,
	}
}

// makePayload constructs payload for the next produced block.
// It waits for any transactions OR until `cfg.BlockInterval` passes.
func (s *State) makePayload(ctx context.Context) (*types.Payload, error) {
	// Wait for transactions. We give up and produce an empty block if mempool is empty for
	// cfg.BlockInterval.
	_ = utils.WithTimeout(ctx, s.cfg.BlockInterval, func(ctx context.Context) error {
		return s.txMempool.TxStore().WaitForTxs(ctx)
	})
	// If the context has been cancelled though, we just fail.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	txs, gasEstimated := s.txMempool.ReapTxs(mempool.ReapLimits{
		MaxTxs:          utils.Some(min(types.MaxTxsPerBlock, s.cfg.maxTxsPerBlock())),
		MaxBytes:        utils.Some(utils.Clamp[int64](types.MaxTxsBytesPerBlock)),
		MaxGasWanted:    utils.Some(s.cfg.MaxGasPerBlockI64()),
		MaxGasEstimated: utils.Some(s.cfg.MaxGasPerBlockI64()),
	}, true)
	payloadTxs := make([][]byte, 0, len(txs))
	for _, tx := range txs {
		payloadTxs = append(payloadTxs, tx)
	}
	payload, err := types.PayloadBuilder{
		CreatedAt: time.Now(),
		TotalGas: uint64(gasEstimated), // nolint:gosec // always non-negative
		Txs:      payloadTxs,
	}.Build()
	// This should never happen: we construct the payload from correctly sized data.
	if err != nil {
		panic(fmt.Errorf("PayloadBuilder{}.Build(): %w", err))
	}
	return payload, nil
}

// nextPayload constructs the payload for the next block.
// Wrapper of makePayload which ensures that the block is not empty (if required).
func (s *State) nextPayload(ctx context.Context) (*types.Payload, error) {
	for {
		payload, err := s.makePayload(ctx)
		if err != nil {
			return nil, err
		}
		if len(payload.Txs()) > 0 || s.cfg.AllowEmptyBlocks {
			return payload, nil
		}
	}
}

// Run runs the background tasks of the producer state.
func (s *State) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		// Construct blocks from mempool.
		limit := rate.Inf
		burst := 1
		if l, ok := s.cfg.MaxTxsPerSecond.Get(); ok {
			limit = rate.Limit(l)
			burst = int(l + s.cfg.MaxTxsPerBlock) // nolint:gosec
		}
		limiter := rate.NewLimiter(limit, burst)
		for {
			if err := s.consensus.WaitForCapacity(ctx); err != nil {
				return fmt.Errorf("s.Data().WaitForCapacity(): %w", err)
			}
			payload, err := s.nextPayload(ctx)
			if err != nil {
				return fmt.Errorf("s.nextPayload(): %w", err)
			}
			if _, err := s.consensus.ProduceBlock(ctx, payload); err != nil {
				return fmt.Errorf("s.Data().PushBlock(): %w", err)
			}
			if err := limiter.WaitN(ctx, len(payload.Txs())); err != nil {
				return fmt.Errorf("limiter(): %w", err)
			}
		}
	})
}
