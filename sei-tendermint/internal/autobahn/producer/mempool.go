package producer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var errTooLarge = errors.New("transaction too large")
var errBadNonce = errors.New("bad nonce")
var ErrNotProducing = errors.New("not producing")

type pendingTx struct {
	addr         common.Address
	nonceRangeID nonceRangeID
}

func (r *nonceRange) Len() uint64 { return r.next - r.first }

type blockSpec struct {
	gasEstimated uint64
	gasWanted    uint64
	sizeBytes    uint64
	rawTxs       [][]byte
	txs          []*pendingTx
}

// mempool is one produce session. State.mempool publishes it; nil means idle.
type mempool struct {
	inner utils.Watch[*mempoolInner]
}

// Incarnation of nonceRange of pending txs.
// Tx with the same nonce can be inserted multiple times
// when pending tx get invalidated, but each one will be
// added to a nonceRange with different ID.
type nonceRangeID uint64

// range [first,next) of pending txs in lane + mempool
type nonceRange struct {
	id    nonceRangeID
	first uint64
	next  uint64
}

// mempoolInner is the lock-protected session state. The Watch value is fixed for
// the session; closed ends the session without replacing that pointer.
type mempoolInner struct {
	closed           bool
	capacity         uint64
	lane             types.LaneID
	first            types.LaneHeight
	next             types.LaneHeight
	blocks           map[types.LaneHeight]*blockSpec
	nextBlock        *blockSpec
	nextNonceRangeID nonceRangeID
	evmNonces        map[common.Address]*nonceRange // ranges of pending txs by sender.
}

func newMempool(capacity uint64, lane types.LaneID, n types.LaneHeight) *mempool {
	return &mempool{
		inner: utils.NewWatch(&mempoolInner{
			capacity:  capacity,
			lane:      lane,
			first:     n,
			next:      n,
			blocks:    map[types.LaneHeight]*blockSpec{},
			nextBlock: &blockSpec{},
			evmNonces: map[common.Address]*nonceRange{},
		}),
	}
}

func (m *mempoolInner) IsFull() bool {
	return uint64(m.next-m.first) >= m.capacity && len(m.nextBlock.txs) > 0
}

func (m *mempoolInner) CanSealBlock(allowEmpty bool) bool {
	return uint64(m.next-m.first) < m.capacity && (allowEmpty || len(m.nextBlock.txs) > 0)
}

func (m *mempoolInner) SealBlock() {
	m.blocks[m.next] = m.nextBlock
	m.next += 1
	m.nextBlock = &blockSpec{}
}

func (m *mempool) pendingNonces(addr common.Address) (nonceRange, bool) {
	for inner := range m.inner.Lock() {
		if r, ok := inner.evmNonces[addr]; ok {
			return *r, ok
		}
	}
	return nonceRange{}, false
}

func (s *State) evmNextPendingNonce(m *mempool, addr common.Address) uint64 {
	// IMPORTANT: first snapshot the pending nonces THEN query app nonce.
	// There would be a race condition otherwise.
	pendingNonces, ok := m.pendingNonces(addr)
	// The committed nonce may trail the last finalized block, but never a pruned one: a lane block
	// is pruned only once its AppHash is pushed, which follows its commit.
	appNonce := s.app.EvmCommittedNonce(addr)

	if !ok {
		// no pending txs.
		return appNonce
	}
	if pendingNonces.first <= appNonce {
		// pending txs and no gap.
		return pendingNonces.next
	}
	// Nonce gap detected, invalidate pending nonces.
	for inner, ctrl := range m.inner.Lock() {
		// Check ID, since pending nonces could have been invalidated by a concurrent call.
		if got, ok := inner.evmNonces[addr]; ok && got.id == pendingNonces.id {
			delete(inner.evmNonces, addr)
			ctrl.Updated()
		}
	}
	return appNonce
}

func (s *State) EvmNextPendingNonce(addr common.Address) uint64 {
	mp, ok := s.mempool.Load().Get()
	if !ok {
		return s.app.EvmNonce(addr)
	}
	return s.evmNextPendingNonce(mp, addr)
}

// Removes txs from mempool assigned to lane blocks <n.
func (s *State) pruneMempool(mp *mempool, n types.LaneHeight) {
	for m, ctrl := range mp.inner.Lock() {
		if m.closed || n < m.first {
			return
		}
		ctrl.Updated()
		for m.first < min(n, m.next) {
			b := m.blocks[m.first]
			delete(m.blocks, m.first)
			m.first += 1
			for _, tx := range b.txs {
				// evmNonces entry could be overridden in case
				// pending txs were invalidated, and new pending txs were added.
				if r, ok := m.evmNonces[tx.addr]; ok && r.id == tx.nonceRangeID {
					// blocks are constructed in nonce order, so
					// nonce of tx is tx.nonceRange.first at this point.
					r.first += 1
					if r.Len() == 0 {
						delete(m.evmNonces, tx.addr)
					}
				}
			}
		}
		// n > m.next shouldn't really happen,
		// because local mempool is the only source of local lane blocks,
		// but we handle it gracefully anyway.
		m.next = max(m.next, n)
	}
}

// WaitForMempool waits until an active produce session is available or ctx is canceled.
func (s *State) WaitForMempool(ctx context.Context) error {
	_, err := s.mempool.Wait(ctx, func(mp utils.Option[*mempool]) bool { return mp.IsPresent() })
	return err
}

// checkTx runs the app CheckTx for tx, holding one of cfg.MaxConcurrentCheckTx permits
// for the duration of the call. Waiting for a permit is cancelled with ctx.
func (s *State) checkTx(ctx context.Context, tx tmtypes.Tx) (abevm.EvmTx, abevm.AdmittedTx, error) {
	start := time.Now()
	release, err := s.checkTxSem.Acquire(ctx)
	metrics.ObserveCheckTxWait(time.Since(start))
	if err != nil {
		return abevm.EvmTx{}, abevm.AdmittedTx{}, err
	}
	defer release()
	return abevm.CheckTx(s.app, s.signer, tx)
}

// InsertTx inserts tx into the mempool, waiting for capacity when full.
// Concurrent calls waiting for capacity are admitted in arrival order.
func (s *State) InsertTx(ctx context.Context, tx tmtypes.Tx) (abevm.AdmittedTx, error) {
	resp, err := s.doInsertTx(ctx, tx)
	var result string
	switch {
	case errors.Is(err, errTooLarge):
		result = "too_large"
	case errors.Is(err, ErrNotProducing):
		result = "not_producing"
	case errors.Is(err, errBadNonce):
		result = "bad_nonce"
	case errors.Is(err, abevm.ErrTxRejected):
		result = "rejected"
	case err != nil:
		result = "error"
	default:
		result = "ok"
	}
	metrics.Global.InsertsAt(result).Add(1)
	return resp, err
}

// Inserts transaction. Blocks until there is capacity in the mempool.
func (s *State) doInsertTx(ctx context.Context, tx tmtypes.Tx) (abevm.AdmittedTx, error) {
	mp, ok := s.mempool.Load().Get()
	if !ok {
		return abevm.AdmittedTx{}, ErrNotProducing
	}
	if uint64(len(tx)) > types.MaxTxsBytesPerLaneBlock {
		return abevm.AdmittedTx{}, errTooLarge
	}
	decoded, resp, err := s.checkTx(ctx, tx)
	if err != nil {
		return abevm.AdmittedTx{}, err
	}

	gasWanted := decoded.Gas()
	if gasWanted > s.cfg.MaxGasWantedPerBlock {
		return abevm.AdmittedTx{}, errTooLarge
	}
	// Normalize the gas estimate.
	gasEstimated := resp.GasEstimated
	if gasEstimated < minTxGas || gasEstimated > gasWanted {
		gasEstimated = gasWanted
	}
	if gasEstimated > s.cfg.MaxGasEstimatedPerBlock {
		return abevm.AdmittedTx{}, errTooLarge
	}

	// Quick check the evm pending nonce.
	// It has a side effect of resetting the pending nonce, if gap is detected.
	addr := decoded.Sender()
	if got, want := decoded.Nonce(), s.evmNextPendingNonce(mp, addr); got != want {
		return abevm.AdmittedTx{}, fmt.Errorf("%w: got %v, want %v", errBadNonce, got, want)
	}
	// Rate limiting for incoming transactions.
	// This is lane throughput throttling, so it comes after tx validation.
	// Also it is TESTONLY feature as of today, noop in prod.
	if err := s.insertTxLimiter.Wait(ctx); err != nil {
		return abevm.AdmittedTx{}, err
	}
	capacityWaitStart := time.Now()
	// In case there are many concurrent insertTx calls waiting for capacity,
	// it gets CPU heavy to wake all of them every time mempool gets updated.
	// Therefore we guard the inner mutex with a semaphore which wakes 1 task at a time.
	// It is ok, because all waiting insertTx calls wait on exactly the same condition (capacity).
	release, err := s.insertTxSem.Acquire(ctx)
	if err != nil {
		return abevm.AdmittedTx{}, err
	}
	defer release()
	for m, ctrl := range mp.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return m.closed || !m.IsFull() }); err != nil {
			return abevm.AdmittedTx{}, err
		}
		metrics.ObserveCapacityWait(time.Since(capacityWaitStart))
		if m.closed {
			return abevm.AdmittedTx{}, ErrNotProducing
		}
		// Verify the nonce again under lock.
		// Here we have a race condition between app execution and evmNonces updates.
		// This is fine, as our pending nonce tracking is always consistent,
		// and converges to correct state if sender's txs are submitted sequentially in nonce order.
		if r, ok := m.evmNonces[addr]; ok {
			if r.next != decoded.Nonce() {
				return abevm.AdmittedTx{}, fmt.Errorf("%w: got %v, want %v", errBadNonce, decoded.Nonce(), r.next)
			}
			r.next += 1
		} else {
			m.evmNonces[addr] = &nonceRange{m.nextNonceRangeID, decoded.Nonce(), decoded.Nonce() + 1}
			m.nextNonceRangeID += 1
		}
		// If any limit would be exceeded, then construct a payload.
		// Note that we use subtraction in a way avoiding arithmetic overflows.
		ok := s.cfg.maxTxsPerBlock()-uint64(len(m.nextBlock.txs)) >= 1
		ok = ok && types.MaxTxsBytesPerLaneBlock-m.nextBlock.sizeBytes >= uint64(len(tx))
		ok = ok && s.cfg.MaxGasWantedPerBlock-m.nextBlock.gasWanted >= gasWanted
		ok = ok && s.cfg.MaxGasEstimatedPerBlock-m.nextBlock.gasEstimated >= gasEstimated
		if !ok {
			m.SealBlock()
		}
		if len(m.nextBlock.txs) == 0 {
			// We notify that we start a new block.
			ctrl.Updated()
		}

		b := m.nextBlock
		b.gasEstimated += gasEstimated
		b.gasWanted += decoded.Gas()
		b.sizeBytes += uint64(len(tx))
		b.rawTxs = append(b.rawTxs, tx)
		p := &pendingTx{
			addr:         addr,
			nonceRangeID: m.evmNonces[addr].id,
		}
		b.txs = append(b.txs, p)
	}
	return resp, nil
}
