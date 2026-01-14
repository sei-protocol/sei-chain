package producer

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-stream/pkg/service"
	"github.com/sei-protocol/sei-stream/pkg/utils"
	"github.com/tendermint/tendermint/internal/autobahn/consensus"
	"github.com/tendermint/tendermint/internal/autobahn/pkg/protocol"
	"github.com/tendermint/tendermint/internal/autobahn/types"
)

// Config is the config of the block service.
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

// State is the block producer state.
type State struct {
	cfg *Config
	// channel of transactions to build the next block from.
	mempool chan *protocol.Transaction
	// consensus state to which published blocks will be reported.
	consensus *consensus.State
}

// NewState constructs a new block producer state.
// Returns an error if the current node is NOT a producer.
func NewState(cfg *Config, consensus *consensus.State) *State {
	return &State{
		cfg:       cfg,
		mempool:   make(chan *protocol.Transaction, cfg.MempoolSize),
		consensus: consensus,
	}
}

// makePayload constructs payload for the next produced block.
// It waits for enough transactions OR until `cfg.BlockInterval` passes.
func (s *State) makePayload(ctx context.Context) *types.Payload {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.BlockInterval)
	defer cancel()
	maxTxs := s.cfg.maxTxsPerBlock()
	var totalGas uint64
	var txs [][]byte
	for totalGas < s.cfg.MaxGasPerBlock && uint64(len(txs)) < maxTxs {
		tx, err := utils.Recv(ctx, s.mempool)
		if err != nil {
			break
		}
		txs = append(txs, tx.Payload)
		totalGas += tx.GasUsed
	}
	return types.PayloadBuilder{
		CreatedAt: time.Now(),
		TotalGas:  totalGas,
		Txs:       txs,
	}.Build()
}

// nextPayload constructs the payload for the next block.
// Wrapper of makePayload which ensures that the block is not empty (if required).
func (s *State) nextPayload(ctx context.Context) (*types.Payload, error) {
	for {
		payload := s.makePayload(ctx)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if len(payload.Txs()) > 0 || s.cfg.AllowEmptyBlocks {
			return payload, nil
		}
	}
}

// PushToMempool pushes the transaction to the mempool.
func (s *State) PushToMempool(ctx context.Context, tx *protocol.Transaction) error {
	return utils.Send(ctx, s.mempool, tx)
}

// Run runs the background tasks of the producer state.
func (s *State) Run(ctx context.Context) error {
	return utils.IgnoreCancel(service.Run(ctx, func(ctx context.Context, scope service.Scope) error {
		// Construct blocks from mempool.
		var limiter utils.Option[*utils.Limiter]
		if rate, ok := s.cfg.MaxTxsPerSecond.Get(); ok {
			burst := rate + s.cfg.MaxTxsPerBlock
			l := utils.NewLimiter(rate, burst)
			scope.Spawn(func() error { return l.Run(ctx) })
			limiter = utils.Some(l)
		}
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
			if limiter, ok := limiter.Get(); ok {
				if err := limiter.Acquire(ctx, uint64(len(payload.Txs()))); err != nil {
					return fmt.Errorf("limiter(): %w", err)
				}
			}
		}
	}))
}
