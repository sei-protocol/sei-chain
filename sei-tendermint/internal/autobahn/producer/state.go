package producer

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/time/rate"
)

// Config is the config of the block scope.
type Config struct {
	MaxGasPerBlock   uint64
	MaxTxsPerBlock   uint64
	// Delay after which a non-full block can be produced.
	BlockInterval    time.Duration
	AllowEmptyBlocks bool
	// TESTONLY: max rate at which lane is produced. It can be used to do
	// benchmarks with stable throughput, in case execution performance degrades
	// when overloaded.
	MaxTxsPerSecond  utils.Option[uint64]
}

const minTxGas = 21000

func (c *Config) maxTxsPerBlock() uint64 {
	return min(types.MaxTxsPerBlock, c.MaxTxsPerBlock)
}

// State is the block producer state.
type State struct {
	app     *proxy.Proxy
	cfg      *Config
	mempool utils.Watch[*mempool]
	// consensus state to which published blocks will be reported.
	consensus *consensus.State
}
// NewState constructs a new block producer state.
// Returns an error if the current node is NOT a producer.
func NewState(cfg *Config, consensus *consensus.State) *State {
	return &State{
		cfg:       cfg,
		mempool: utils.NewWatch(&mempool {
			evmAccounts: map[common.Address]*evmAccount{},
		}),
		consensus: consensus,
	}
}

func (s *State) setNextToExecute(n types.BlockNumber) {
	for m,ctrl := range s.mempool.Lock() {
		if n < m.nextToExecute { return }
		ctrl.Updated()
		m.nextToExecute = n
		for addr,acc := range m.evmAccounts {
			if wantMin,ok := acc.nonceByBlock.Prune(n); acc.nonceByBlock.Len()==0 || (ok && s.app.EvmNonce(addr) < wantMin) {
				delete(m.evmAccounts,addr)
			}
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
		dataState := s.consensus.Data()
		availState := s.consensus.Avail()
		lane := availState.PublicKey()
		
		for m := range s.mempool.Lock() {
			m.nextToExecute = 0
			m.nextToProduce = availState.NextBlock(lane)
		}

		scope.Spawn(func() error {
			next := types.BlockNumber(0)
			var err error
			for { 
				if next,err = dataState.WaitUntilExecuted(ctx,lane,next); err!=nil { return err }
				s.setNextToExecute(next)
			}
		})
		lastBlockTime := time.Now()
		for {	
			if _,err := nextToExecute.Wait(ctx, func(next types.BlockNumber) bool { return next + avail.BlocksPerLane > n }); err!=nil {
				return err
			}
			if err := availState.WaitForCapacity(ctx); err != nil {
				return fmt.Errorf("s.consensus.Avail().WaitForCapacity(): %w", err)
			}
			// Wait until either
			// * there is a full proposal in mempool
			// * BlockInterval since the last block passed AND (AllowEmptyBlocks OR mempool is non-empty)
			for m,ctrl := range s.mempool.Lock() {
				// First just wait for full proposal with timeout (first condition)
				_ = utils.WithDeadline(ctx, utils.Some(lastBlockTime.Add(s.cfg.BlockInterval)), func(ctx context.Context) error {
					return ctrl.WaitUntil(ctx, func() bool { return m.nextPayload.IsPresent() })
				})
				if ctx.Err()!=nil {
					return ctx.Err()
				}
				// Then wait for ANY condition.
				if err:=ctrl.WaitUntil(ctx, func() bool {
					return m.nextPayload.IsPresent() || s.cfg.AllowEmptyBlocks || len(m.txs) > 0
				}); err!=nil {
					return err
				}
				// Construct the payload unconditionally.
				m.buildPayload()
			}
			
			if _, err := availState.ProduceBlock(ctx, payload); err != nil {
				return fmt.Errorf("availState.ProduceBlock(): %w", err)
			}
			if err := limiter.WaitN(ctx, len(payload.Txs())); err != nil {
				return fmt.Errorf("limiter(): %w", err)
			}
		}
	})
}
