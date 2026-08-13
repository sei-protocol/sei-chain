package producer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"golang.org/x/time/rate"
)

// Config is the config of the block scope.
type Config struct {
	MaxGasWantedPerBlock    uint64
	MaxGasEstimatedPerBlock uint64
	MaxTxsPerBlock          uint64
	AllowEmptyBlocks        bool
	// Delay after which a non-full block can be produced.
	BlockInterval time.Duration
	// TESTONLY: max rate at which lane is produced. It can be used to do
	// benchmarks with stable throughput, in case execution performance degrades
	// when overloaded.
	MaxTxsPerSecond utils.Option[uint64]
}

const minTxGas = 21000

func (c *Config) maxTxsPerBlock() uint64 {
	return min(types.MaxTxsPerBlock, c.MaxTxsPerBlock)
}

// State is the block producer state.
type State struct {
	cfg *Config
	app *proxy.Proxy
	// mempool.inner is None when not producing.
	mempool   utils.Watch[*mempool]
	consensus *consensus.State
}

// NewState constructs a new block producer state.
// Mempool starts None; alignMempool creates it for each produce session.
func NewState(cfg *Config, consensus *consensus.State, app *proxy.Proxy) *State {
	return &State{
		cfg:       cfg,
		app:       app,
		mempool:   utils.NewWatch(&mempool{}),
		consensus: consensus,
	}
}

// alignMempool installs a fresh session mempool for lane.
func (s *State) alignMempool(lane types.LaneID) (*mempoolInner, types.BlockNumber) {
	n := s.consensus.Avail().NextBlock(lane)
	for mp, ctrl := range s.mempool.Lock() {
		m := newMempoolInner(avail.BlocksPerLane, lane, n)
		mp.inner = utils.Some(m)
		ctrl.Updated()
		return m, n
	}
	panic("unreachable")
}

func (s *State) clearMempool() {
	for mp, ctrl := range s.mempool.Lock() {
		mp.inner = utils.None[*mempoolInner]()
		ctrl.Updated()
	}
}

// Run runs the background tasks of the producer state:
// * prunes executed lane blocks from mempool
// * pushes new lane blocks from mempool to avail state
// Note that mempool capacity bounds the number of unexecuted blocks of the local lane.
// This is needed so that we can track the evm nonces of sequenced txs - mempool admits txs
// sequentially in the nonce order.
func (s *State) Run(ctx context.Context) error {
	availState := s.consensus.Avail()
	for ctx.Err() == nil {
		lane, err := availState.WaitForLocalLane(ctx)
		if err != nil {
			return err
		}

		// WaitUntilClosed ends the session; runMempool is background and torn down with it.
		err = utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, sc scope.Scope) error {
			sc.SpawnBg(func() error {
				return utils.IgnoreCancel(s.runMempool(ctx, availState, lane))
			})
			return availState.WaitUntilClosed(ctx, lane)
		}))
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (s *State) runMempool(ctx context.Context, availState *avail.State, lane types.LaneID) error {
	m, firstBlock := s.alignMempool(lane)
	defer s.clearMempool()
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		scope.Spawn(func() error {
			// Task pruning executed lane blocks from the mempool
			dataState := s.consensus.Data()
			var err error
			for toExecute := firstBlock; ; {
				if toExecute, err = dataState.WaitUntilExecuted(ctx, lane, toExecute); err != nil {
					return err
				}
				s.pruneMempool(m, toExecute)
			}
		})
		scope.Spawn(func() error {
			// Task pushing blocks from mempool to avail state.
			limit := rate.Inf
			burst := 1
			if l, ok := s.cfg.MaxTxsPerSecond.Get(); ok {
				limit = rate.Limit(l)
				burst = int(l + s.cfg.MaxTxsPerBlock) // nolint:gosec
			}
			limiter := rate.NewLimiter(limit, burst)
			for toProduce := firstBlock; ; toProduce += 1 {
				if err := availState.WaitForCapacity(ctx, lane, toProduce); err != nil {
					if errors.Is(err, avail.ErrBadLane) {
						return nil
					}
					return fmt.Errorf("availState.WaitForCapacity(): %w", err)
				}
				var payload *types.Payload
				// Wait until either
				// * there is a full proposal in mempool
				// * BlockInterval since the last block passed AND (AllowEmptyBlocks OR mempool is non-empty)
				for m, ctrl := range s.mempool.Lock() {
					// Wait for full payload with timeout.
					// Note that in total the time between blocks is WaitForLocalCapacity delay + BlockInterval
					// We don't want to cap them together with BlockInterval, because that will cause production of almost empty blocks.
					if err := utils.WithTimeout(ctx, s.cfg.BlockInterval, func(ctx context.Context) error {
						return ctrl.WaitUntil(ctx, func() bool { return toProduce < m.next })
					}); err != nil {
						if ctx.Err() != nil {
							return ctx.Err()
						}
						// Wait for non-empty payload.
						if err := ctrl.WaitUntil(ctx, func() bool {
							return toProduce < m.next || (toProduce == m.next && m.CanSealBlock(s.cfg.AllowEmptyBlocks))
						}); err != nil {
							return err
						}
						// Seal the payload if needed.
						if toProduce == m.next {
							m.SealBlock()
							ctrl.Updated()
						}
					}
					b, ok := m.blocks[toProduce]
					if !ok {
						// Block number tracking should always be in sync between avail state and mempool:
						// * mempool keeps blocks until they are executed.
						// * blocks can be executed only after they are included in the lane.
						// * lane is populated from the mempool.
						return fmt.Errorf("mempool mismatched block production")
					}
					var err error
					payload, err = types.PayloadBuilder{
						CreatedAt:         time.Now(),
						TotalGasWanted:    b.gasWanted,
						TotalGasEstimated: b.gasEstimated,
						Txs:               b.txs,
					}.Build()
					if err != nil {
						// This should never happen: we construct the payload from correctly sized data.
						panic(fmt.Errorf("PayloadBuilder{}.Build(): %w", err))
					}
				}
				if _, err := availState.ProduceLocalBlock(lane, toProduce, payload); err != nil {
					if errors.Is(err, avail.ErrBadLane) {
						return nil
					}
					return fmt.Errorf("availState.ProduceLocalBlock(): %w", err)
				}
				// TODO(gprusak): move this limit to insertTx instead.
				if err := limiter.WaitN(ctx, len(payload.Txs())); err != nil {
					return fmt.Errorf("limiter(): %w", err)
				}
			}
		})
		return nil
	})
}
