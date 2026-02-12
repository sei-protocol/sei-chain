package app

import (
	"fmt"
	"math/big"
	"sync"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	evmante "github.com/sei-protocol/sei-chain/x/evm/ante"
)

// senderRecoveryResult holds the output of an ECDSA sender recovery.
type senderRecoveryResult struct {
	sender  common.Address
	seiAddr sdk.AccAddress
	pubkey  cryptotypes.PubKey
	err     error
}

// SenderRecoverer manages parallel ECDSA sender recovery with per-transaction
// blocking granularity. Call Recover() to start a non-blocking recovery for a
// given tx index, then Get() to retrieve the result — blocking only until that
// specific transaction's recovery completes.
type SenderRecoverer struct {
	height  int64
	results []senderRecoveryResult
	ready   []chan struct{} // nil = no recovery requested; closed = result available
	wg      sync.WaitGroup  // for cleanup: WaitAll blocks until every goroutine exits
}

// Recover starts a non-blocking sender recovery for the transaction at idx.
// The caller must not call Recover twice for the same idx.
func (r *SenderRecoverer) Recover(idx int, ctx sdk.Context, ethTx *ethtypes.Transaction, chainID *big.Int) {
	ch := make(chan struct{})
	r.ready[idx] = ch
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer close(ch)
		defer func() {
			if p := recover(); p != nil {
				r.results[idx] = senderRecoveryResult{
					err: fmt.Errorf("panic during sender recovery: %v", p),
				}
			}
		}()
		sender, seiAddr, pubkey, err := evmante.RecoverSenderFromEthTx(ctx, ethTx, chainID)
		r.results[idx] = senderRecoveryResult{sender, seiAddr, pubkey, err}
	}()
}

// IsRecovering returns true if a recovery has been requested for idx (in-flight
// or already complete). This is a non-blocking check — use Get() to obtain the
// actual result (which blocks until the recovery finishes).
func (r *SenderRecoverer) IsRecovering(idx int) bool {
	return r != nil && idx >= 0 && idx < len(r.ready) && r.ready[idx] != nil
}

// Get returns the recovered sender for idx. If recovery is in flight it blocks
// until that specific transaction completes. Returns nil if no recovery was
// requested for idx (non-EVM tx, decode error, etc.), signaling the caller to
// fall back to inline recovery.
func (r *SenderRecoverer) Get(idx int) *senderRecoveryResult {
	if r == nil || idx < 0 || idx >= len(r.ready) || r.ready[idx] == nil {
		return nil
	}
	<-r.ready[idx]
	return &r.results[idx]
}

// WaitAll blocks until every in-flight recovery goroutine has finished.
// Safe to call from multiple goroutines. Used for cleanup before returning
// the recoverer to the pool.
func (r *SenderRecoverer) WaitAll() {
	if r != nil {
		r.wg.Wait()
	}
}

// SenderRecovererPool recycles SenderRecoverer instances across blocks.
type SenderRecovererPool struct {
	pool sync.Pool
}

func NewSenderRecovererPool() *SenderRecovererPool {
	return &SenderRecovererPool{
		pool: sync.Pool{
			New: func() interface{} {
				return &SenderRecoverer{
					results: make([]senderRecoveryResult, 0, 512),
					ready:   make([]chan struct{}, 0, 512),
				}
			},
		},
	}
}

// Get returns a recoverer sized for txCount with all entries reset.
func (p *SenderRecovererPool) Get(height int64, txCount int) *SenderRecoverer {
	r := p.pool.Get().(*SenderRecoverer)
	r.height = height

	if cap(r.results) >= txCount {
		r.results = r.results[:txCount]
	} else {
		r.results = make([]senderRecoveryResult, txCount)
	}
	for i := range r.results {
		r.results[i] = senderRecoveryResult{}
	}

	// Channels cannot be reused after close — allocate fresh each time.
	r.ready = make([]chan struct{}, txCount)

	return r
}

// Put returns a recoverer to the pool. Oversized instances (>8192 txs) are
// discarded to avoid pooling abnormally large blocks.
func (p *SenderRecovererPool) Put(r *SenderRecoverer) {
	if r == nil || cap(r.results) > 8192 {
		return
	}
	r.height = 0
	r.results = r.results[:0]
	r.ready = nil
	p.pool.Put(r)
}
