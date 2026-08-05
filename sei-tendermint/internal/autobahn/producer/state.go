package producer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
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
	cfg     *Config
	app     *proxy.Proxy
	mempool utils.Watch[*mempool]
	// consensus state to which published blocks will be reported.
	consensus *consensus.State
}

// NewState constructs a new block producer state.
// The mempool tip is aligned to the current applied LocalLane when present;
// otherwise it starts empty and is reset when a LaneID streak begins.
func NewState(cfg *Config, consensus *consensus.State, app *proxy.Proxy) *State {
	n := types.BlockNumber(0)
	laneOpt := consensus.Avail().LocalLane()
	if lane, ok := laneOpt.Get(); ok {
		n = consensus.Avail().NextBlock(lane)
	}
	return &State{
		cfg: cfg,
		app: app,
		mempool: utils.NewWatch(&mempool{
			capacity:  avail.BlocksPerLane,
			lane:      laneOpt,
			first:     n,
			next:      n,
			blocks:    map[types.BlockNumber]*blockSpec{},
			nextBlock: &blockSpec{evmNonces: map[common.Address]uint64{}},
			evmNonces: map[common.Address]uint64{},
			evmTxs:    map[common.Hash]tmtypes.Tx{},
		}),
		consensus: consensus,
	}
}

func (s *State) alignMempoolForLane(lane types.LaneID) types.BlockNumber {
	n := s.consensus.Avail().NextBlock(lane)
	for m, ctrl := range s.mempool.Lock() {
		if cur, ok := m.lane.Get(); ok && cur == lane {
			// Continuous streak (including first Run after NewState): keep tip + txs.
			return m.first
		}
		// New LaneID streak: tip from avail for this lane.
		m.lane = utils.Some(lane)
		m.first = n
		m.next = n
		m.blocks = map[types.BlockNumber]*blockSpec{}
		m.nextBlock = &blockSpec{evmNonces: map[common.Address]uint64{}}
		m.evmNonces = map[common.Address]uint64{}
		m.evmTxs = map[common.Hash]tmtypes.Tx{}
		ctrl.Updated()
	}
	return n
}

// Run runs the background tasks of the producer state:
// * prunes executed lane blocks from mempool
// * pushes new lane blocks from mempool to avail state
// Note that mempool capacity bounds the number of unexecuted blocks of the local lane.
// This is needed so that we can track the evm nonces of sequenced txs - mempool admits txs
// sequentially in the nonce order.
//
// Membership is epoch-scoped: stay continues the same LaneID streak; leave cancels
// the streak; a later join starts a new Run for the new (v, e_join).
func (s *State) Run(ctx context.Context) error {
	availState := s.consensus.Avail()
	return availState.LocalLaneUpdates().Iter(ctx, func(ctx context.Context, laneOpt utils.Option[types.LaneID]) error {
		lane, ok := laneOpt.Get()
		if !ok {
			<-ctx.Done()
			return ctx.Err()
		}
		return s.runLaneStreak(ctx, availState, lane)
	})
}

func (s *State) runLaneStreak(ctx context.Context, availState *avail.State, lane types.LaneID) error {
	firstBlock := s.alignMempoolForLane(lane)
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		scope.Spawn(func() error {
			// Task pruning executed lane blocks from the mempool
			dataState := s.consensus.Data()
			var err error
			for toExecute := firstBlock; ; {
				if toExecute, err = dataState.WaitUntilExecuted(ctx, lane, toExecute); err != nil {
					return err
				}
				s.pruneMempool(toExecute)
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
			lastBlockTime := time.Now()
			for toProduce := firstBlock; ; toProduce += 1 {
				if err := availState.WaitForLocalCapacity(ctx, lane, toProduce); err != nil {
					return s.streakOpErr(lane, "availState.WaitForLocalCapacity()", err)
				}
				var payload *types.Payload
				// Wait until either
				// * there is a full proposal in mempool
				// * BlockInterval since the last block passed AND (AllowEmptyBlocks OR mempool is non-empty)
				for m, ctrl := range s.mempool.Lock() {
					// Wait for full payload with timeout.
					if err := utils.WithDeadline(ctx, utils.Some(lastBlockTime.Add(s.cfg.BlockInterval)), func(ctx context.Context) error {
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
				if _, err := availState.ProduceLocalBlock(toProduce, payload); err != nil {
					return s.streakOpErr(lane, "availState.ProduceLocalBlock()", err)
				}
				lastBlockTime = time.Now()
				if err := limiter.WaitN(ctx, len(payload.Txs())); err != nil {
					return fmt.Errorf("limiter(): %w", err)
				}
			}
		})
		return nil
	})
}

// streakOpErr maps leave-race ErrBadLane onto context.Canceled so LocalLane
// Iter continues (rejoin) instead of permanently killing producer.Run.
// Covers both: LocalLane already Store'd, and maps/epoch swapped before Store.
func (s *State) streakOpErr(lane types.LaneID, op string, err error) error {
	if errors.Is(err, avail.ErrBadLane) {
		availState := s.consensus.Avail()
		if got, ok := availState.LocalLane().Get(); !ok || got != lane {
			return context.Canceled
		}
		if !availState.HasLane(lane) {
			return context.Canceled
		}
	}
	return fmt.Errorf("%s: %w", op, err)
}
