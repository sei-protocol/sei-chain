package producer

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
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
	return min(types.MaxTxsPerBlock, c.MaxTxsPerBlock, c.MaxGasPerBlock/minTxGas)
}

// State is the block producer state.
type State struct {
	cfg       *Config
	mempool *Mempool
	// consensus state to which published blocks will be reported.
	consensus *consensus.State
}
// NewState constructs a new block producer state.
// Returns an error if the current node is NOT a producer.
func NewState(cfg *Config, consensus *consensus.State) *State {
	return &State{
		cfg:       cfg,
		mempool:   NewMempool(nil/*TODO*/,cfg.MempoolSize),
		consensus: consensus,
	}
}

func (s *State) MarkExecuted(ctx context.Context, h *types.BlockHeader) error {
	// Producer only cares about executed lane blocks,
	// at which it verifies nonce progress.
	if h.Lane()!=s.consensus.Avail().PublicKey() {
		return nil
	}
	return s.mempool.MarkExecuted(ctx, h.BlockNumber())
}

// nextPayload constructs payload for the next produced block.
// It waits for any transactions OR until `cfg.BlockInterval` passes.
func (s *State) nextPayload(ctx context.Context) (*types.Payload, error) {
	// Wait for transactions. We give up and produce an empty block if mempool is empty for
	// cfg.BlockInterval.
	if s.cfg.AllowEmptyBlocks {
		var cancel context.CancelFunc
		ctx,cancel = context.WithTimeout(ctx, s.cfg.BlockInterval)
		defer cancel()
	}
	return s.mempool.ReapTxs(ctx)
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
		dataState := s.consensus.Data()
		availState := s.consensus.Avail()
		lane := availState.PublicKey()

		// TODO: this variable can be property of the mempool - initial cutoff of the evm snapshot queue
		nextToExecute := utils.NewAtomicSend(types.BlockNumber(0))
		scope.Spawn(func() error {
			for { 
				n,err := dataState.WaitUntilExecuted(ctx,lane,nextToExecute.Load())
				if err!=nil { return err }
				s.mempool.Update(n)
				nextToExecute.Store(n)
			}
		})
		for {	
			n := availState.NextBlock(lane)
			if _,err := nextToExecute.Wait(ctx, func(next types.BlockNumber) bool { return next + avail.BlocksPerLane > n }); err!=nil {
				return err
			}
			// TODO: we should block pruning of dataState on AppQC as well, in which case WaitForCapacity and previous check would be both based on dataState.
			if err := availState.WaitForCapacity(ctx); err != nil {
				return fmt.Errorf("s.consensus.Avail().WaitForCapacity(): %w", err)
			}
			payload, err := s.nextPayload(ctx)
			if err != nil {
				return fmt.Errorf("s.nextPayload(): %w", err)
			}
			if _, err := availState.ProduceBlock(ctx, payload); err != nil {
				return fmt.Errorf("s.Data().PushBlock(): %w", err)
			}
			if err := limiter.WaitN(ctx, len(payload.Txs())); err != nil {
				return fmt.Errorf("limiter(): %w", err)
			}
		}
	})
}
