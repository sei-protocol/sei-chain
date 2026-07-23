package producer

import (
	"context"
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
// Returns an error if the current node is NOT a producer.
func NewState(cfg *Config, consensus *consensus.State, app *proxy.Proxy) *State {
	lane := consensus.Avail().PublicKey()
	n := consensus.Avail().NextBlock(lane)
	return &State{
		cfg: cfg,
		app: app,
		mempool: utils.NewWatch(&mempool{
			capacity:  avail.BlocksPerLane,
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

// Run runs the background tasks of the producer state:
// * prunes executed lane blocks from mempool
// * pushes new lane blocks from mempool to avail state
// Note that mempool capacity bounds the number of unexecuted blocks of the local lane.
// This is needed so that we can track the evm nonces of sequenced txs - mempool admits txs
// sequentially in the nonce order.
func (s *State) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		availState := s.consensus.Avail()
		lane := availState.PublicKey()
		firstBlock := s.mempoolFirst()
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
			for toProduce := firstBlock; ; toProduce += 1 {
				if err := availState.WaitForLocalCapacity(ctx, toProduce); err != nil {
					return fmt.Errorf("availState.WaitForLocalCapacity(): %w", err)
				}
				var payload *types.Payload
				// Wait until either
				// * there is a full proposal in mempool
				// * BlockInterval since the last block passed AND (AllowEmptyBlocks OR mempool is non-empty)
				for m, ctrl := range s.mempool.Lock() {
					// Wait for full payload with timeout.
					// Note that in total the time between blocks is WaitForLocalCapacity delay + BlockInterval
					// We don't want to cap them together with BlockInterval, because that will cause production of almost empty blocks.
					// TODO(gprusak): double check that it works fine with txs rate limiting.
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
				if _, err := availState.ProduceLocalBlock(toProduce, payload); err != nil {
					return fmt.Errorf("availState.ProduceLocalBlock(): %w", err)
				}
				if err := limiter.WaitN(ctx, len(payload.Txs())); err != nil {
					return fmt.Errorf("limiter(): %w", err)
				}
			}
		})
		return nil
	})
}
