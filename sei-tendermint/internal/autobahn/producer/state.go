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
// It waits for enough transactions OR until `cfg.BlockInterval` passes.
func (s *State) makePayload(ctx context.Context) *types.Payload {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.BlockInterval)
	defer cancel()

	if s.txMempool.NumTxsNotPending() == 0 {
		select {
		case <-ctx.Done():
		case <-s.txMempool.TxsAvailable():
		}
	}

	txs, totalGas := s.txMempool.ReapMaxTxsBytesMaxGas(
		min(types.MaxTxsPerBlock,s.cfg.maxTxsPerBlock()),
		utils.Clamp[int64](types.MaxTxsBytesPerBlock),
		utils.Clamp[int64](s.cfg.MaxGasPerBlock),
		utils.Clamp[int64](s.cfg.MaxGasPerBlock),
	)
	s.txMempool.RemoveTxs(txs)
	payloadTxs := make([][]byte, 0, len(txs))
	for _, tx := range txs {
		payloadTxs = append(payloadTxs, tx)
	}
	payload,err := types.PayloadBuilder{
		CreatedAt: time.Now(),
		// TODO: ReapMaxTxsBytesMaxGas does not handle corner cases correctly rn, which actually
		// can produce negative total gas. Fixing it right away might be backward incompatible afaict,
		// so we leave it as is for now.
		TotalGas: uint64(totalGas), // nolint:gosec
		Txs:      payloadTxs,
	}.Build()
	// This should never happen: we construct the payload from correctly sized data.
	if err!=nil {
		panic(fmt.Errorf("PayloadBuilder{}.Build(): %w",err))
	}
	return payload
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
