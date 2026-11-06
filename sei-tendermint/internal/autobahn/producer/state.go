package producer

import (
	"context"
	"errors"
	"fmt"
	"runtime"
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
	// Max number of CheckTx calls executed concurrently by InsertTx/TryInsertTx.
	// None or Some(0) means half of GOMAXPROCS, at least 1.
	MaxConcurrentCheckTx utils.Option[uint64]
}

func (c *Config) maxConcurrentCheckTx() int {
	if v, ok := c.MaxConcurrentCheckTx.Get(); ok && v > 0 {
		return utils.Clamp[int](v)
	}
	return max(1, runtime.GOMAXPROCS(0)/2)
}

const minTxGas = 21000

func (c *Config) maxTxsPerBlock() uint64 {
	return min(types.MaxTxsPerBlock, c.MaxTxsPerBlock)
}

// State is the block producer state.
type State struct {
	cfg             *Config
	app             *proxy.Proxy
	mempool         utils.AtomicSend[utils.Option[*mempool]] // None when not producing
	consensus       *consensus.State
	checkTxSem      *utils.Semaphore // bounds CheckTx calls.
	insertTxSem     *utils.Semaphore // bounds InsertTx calls waiting for mempool capacity.
	insertTxLimiter *rate.Limiter
}

// NewState constructs a new block producer state.
// Mempool starts None; alignMempool creates it for each produce session.
func NewState(cfg *Config, consensus *consensus.State, app *proxy.Proxy) *State {
	limit := rate.Inf
	burst := 1
	if l, ok := cfg.MaxTxsPerSecond.Get(); ok {
		limit = rate.Limit(l)
		burst = int(l + cfg.MaxTxsPerBlock) // nolint:gosec
	}
	return &State{
		cfg:             cfg,
		app:             app,
		mempool:         utils.NewAtomicSend(utils.None[*mempool]()),
		consensus:       consensus,
		checkTxSem:      utils.NewSemaphore(cfg.maxConcurrentCheckTx()),
		insertTxSem:     utils.NewSemaphore(1),
		insertTxLimiter: rate.NewLimiter(limit, burst),
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
		if err := scope.Run(ctx, func(ctx context.Context, sc scope.Scope) error {
			sc.SpawnBg(func() error {
				return utils.IgnoreCancel(s.runMempool(ctx, availState, lane))
			})
			return availState.WaitUntilClosed(ctx, lane)
		}); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (s *State) runMempool(ctx context.Context, availState *avail.State, lane types.LaneID) error {
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		// Register mempool for lane.
		firstBlock := s.consensus.Avail().NextBlock(lane)
		mp := newMempool(avail.BlocksPerLane, lane, firstBlock)
		s.mempool.Store(utils.Some(mp))
		defer func() {
			s.mempool.Store(utils.None[*mempool]())
			for m, ctrl := range mp.inner.Lock() {
				m.closed = true
				ctrl.Updated()
			}
		}()
		scope.SpawnBg(func() error {
			// Task pruning executed lane blocks from the mempool
			dataState := s.consensus.Data()
			var err error
			for toExecute := firstBlock; ; {
				if toExecute, err = dataState.WaitUntilExecuted(ctx, lane, toExecute); err != nil {
					return err
				}
				s.pruneMempool(mp, toExecute)
			}
		})
		scope.SpawnBg(func() error {
			// Task pushing blocks from mempool to avail state.
			for toProduce := firstBlock; ctx.Err() == nil; toProduce += 1 {
				if err := availState.WaitForCapacity(ctx, lane, toProduce); err != nil {
					if errors.Is(err, avail.ErrLaneClosed) {
						return nil
					}
					return fmt.Errorf("availState.WaitForCapacity(): %w", err)
				}
				var payload *types.Payload
				// Wait until either
				// * there is a full proposal in mempool
				// * BlockInterval since the last block passed AND (AllowEmptyBlocks OR mempool is non-empty)
				for m, ctrl := range mp.inner.Lock() {
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
						Txs:               b.rawTxs,
					}.Build()
					if err != nil {
						// This should never happen: we construct the payload from correctly sized data.
						panic(fmt.Errorf("PayloadBuilder{}.Build(): %w", err))
					}
				}
				if _, err := availState.ProduceLocalBlock(lane, toProduce, payload); err != nil {
					if errors.Is(err, avail.ErrLaneClosed) {
						return nil
					}
					return fmt.Errorf("availState.ProduceLocalBlock(): %w", err)
				}
			}
			return ctx.Err()
		})
		return availState.WaitUntilClosed(ctx, lane)
	})
}
