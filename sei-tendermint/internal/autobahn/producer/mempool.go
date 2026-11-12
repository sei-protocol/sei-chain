package producer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var errTooLarge = errors.New("transaction too large")
var errBadNonce = errors.New("bad nonce")
var errMempoolFull = errors.New("mempool is full")
var ErrNotProducing = errors.New("not producing")

type pendingTx struct {
	raw          tmtypes.Tx
	addr         common.Address
	hash         common.Hash
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
	first            types.BlockNumber
	next             types.BlockNumber
	blocks           map[types.BlockNumber]*blockSpec
	nextBlock        *blockSpec
	nextNonceRangeID nonceRangeID
	evmNonces        map[common.Address]*nonceRange // ranges of pending txs by sender.
	evmTxs           map[common.Hash]*pendingTx     // pendingTxs by hash.
}

func newMempool(capacity uint64, lane types.LaneID, n types.BlockNumber) *mempool {
	return &mempool{
		inner: utils.NewWatch(&mempoolInner{
			capacity:  capacity,
			lane:      lane,
			first:     n,
			next:      n,
			blocks:    map[types.BlockNumber]*blockSpec{},
			nextBlock: &blockSpec{},
			evmNonces: map[common.Address]*nonceRange{},
			evmTxs:    map[common.Hash]*pendingTx{},
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

// TODO(gprusak): this rpc is probably unused, but if it is
// consider whether unsequenced/unexecuted lane txs should be included here.
func (s *State) UnconfirmedTxs() [][]byte {
	mp, ok := s.mempool.Load().Get()
	if !ok {
		return nil
	}
	for m := range mp.inner.Lock() {
		if m.closed {
			return nil
		}
		return m.nextBlock.rawTxs
	}
	panic("unreachable")
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

func (s *State) EvmTxByHash(hash common.Hash) (tmtypes.Tx, bool) {
	if m, ok := s.mempool.Load().Get(); ok {
		for inner := range m.inner.Lock() {
			if tx, ok := inner.evmTxs[hash]; ok {
				return tx.raw, ok
			}
		}
	}
	return nil, false
}

// Removes txs from mempool assigned to lane blocks <n.
func (s *State) pruneMempool(mp *mempool, n types.BlockNumber) {
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
				// evmTxs entry could be overridden in case
				// invalidated pending txs are submitted again.
				if m.evmTxs[tx.hash] == tx {
					delete(m.evmTxs, tx.hash)
				}
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

// TryInsertTx inserts tx to the mempool. Returns errMempoolFull if the mempool is full.
func (s *State) TryInsertTx(ctx context.Context, tx tmtypes.Tx) (*abci.ResponseCheckTx, error) {
	return s.insertTx(ctx, tx, false)
}

// InsertTx inserts tx to the mempool. Blocks if mempool is full; blocked InsertTx calls are
// admitted in arrival order relative to each other, but TryInsertTx calls do not queue and may
// take freed capacity ahead of them.
// The blocked calls are effectively the "unsequenced" part of the mempool.
// After InsertTx returns, the sequence is already scheduled to be included in a lane.
// TODO(gprusak): we might need some prioritization mechanism in case our node can handle more InsertTx calls/s
// than the lane throughput.
func (s *State) InsertTx(ctx context.Context, tx tmtypes.Tx) (*abci.ResponseCheckTx, error) {
	return s.insertTx(ctx, tx, true)
}

// checkTx runs the app CheckTx for tx, holding one of cfg.MaxConcurrentCheckTx permits
// for the duration of the call. Waiting for a permit is cancelled with ctx.
func (s *State) checkTx(ctx context.Context, tx tmtypes.Tx) (*abci.ResponseCheckTxV2, error) {
	start := time.Now()
	release, err := s.checkTxSem.Acquire(ctx)
	metrics.ObserveCheckTxWait(time.Since(start))
	if err != nil {
		return nil, err
	}
	defer release()
	return s.app.CheckTxSafe(ctx, &abci.RequestCheckTxV2{Tx: tx})
}

// insertTx inserts a transaction and records its outcome.
func (s *State) insertTx(ctx context.Context, tx tmtypes.Tx, waitIfFull bool) (*abci.ResponseCheckTx, error) {
	resp, err := s.doInsertTx(ctx, tx, waitIfFull)
	var result string
	switch {
	case errors.Is(err, errTooLarge):
		result = "too_large"
	case errors.Is(err, errMempoolFull):
		result = "full"
	case errors.Is(err, ErrNotProducing):
		result = "not_producing"
	case errors.Is(err, errBadNonce):
		result = "bad_nonce"
	case err != nil:
		result = "error"
	case !resp.IsOK():
		result = "rejected"
	default:
		result = "ok"
	}
	metrics.Global.InsertsAt(result).Add(1)
	return resp, err
}

// Inserts transaction. Blocks until there is capacity in the mempool.
func (s *State) doInsertTx(ctx context.Context, tx tmtypes.Tx, waitIfFull bool) (*abci.ResponseCheckTx, error) {
	mp, ok := s.mempool.Load().Get()
	if !ok {
		return nil, ErrNotProducing
	}
	if uint64(len(tx)) > types.MaxTxsBytesPerBlock {
		return nil, errTooLarge
	}
	// Early exit if mempool is full and we won't wait.
	if !waitIfFull {
		for m := range mp.inner.Lock() {
			if m.IsFull() {
				return nil, errMempoolFull
			}
		}
	}
	resp, err := s.checkTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !resp.IsOK() {
		return resp.ResponseCheckTx, nil
	}
	if !resp.IsEVM {
		return nil, fmt.Errorf("non-evm tx")
	}

	gasWanted := utils.Clamp[uint64](resp.GasWanted)
	if gasWanted > s.cfg.MaxGasWantedPerBlock {
		return nil, errTooLarge
	}
	// Normalize the gas estimate.
	gasEstimated := utils.Clamp[uint64](resp.GasEstimated)
	if gasEstimated < minTxGas || gasEstimated > gasWanted {
		gasEstimated = gasWanted
	}
	if gasEstimated > s.cfg.MaxGasEstimatedPerBlock {
		return nil, errTooLarge
	}

	// Quick check the evm pending nonce.
	// It has a side effect of resetting the pending nonce, if gap is detected.
	addr := resp.EVMSenderAddress
	if got, want := resp.EVMNonce, s.evmNextPendingNonce(mp, addr); got != want {
		return nil, fmt.Errorf("%w: got %v, want %v", errBadNonce, got, want)
	}
	// Rate limiting for incoming transactions.
	// This is lane throughput throttling, so it comes after tx validation.
	// Also it is TESTONLY feature as of today, noop in prod.
	if err := s.insertTxLimiter.Wait(ctx); err != nil {
		return nil, err
	}
	var capacityWaitStart time.Time
	if waitIfFull {
		capacityWaitStart = time.Now()
		// In case there are many concurrent insertTx calls waiting for capacity,
		// it gets CPU heavy to wake all of them every time mempool gets updated.
		// Therefore we guard the inner mutex with a semaphore which wakes 1 task at a time.
		// It is ok, because all waiting insertTx calls wait on exactly the same condition (capacity).
		release, err := s.insertTxSem.Acquire(ctx)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	for m, ctrl := range mp.inner.Lock() {
		if waitIfFull {
			if err := ctrl.WaitUntil(ctx, func() bool { return m.closed || !m.IsFull() }); err != nil {
				return nil, err
			}
			metrics.ObserveCapacityWait(time.Since(capacityWaitStart))
		}
		if m.closed {
			return nil, ErrNotProducing
		}
		if m.IsFull() {
			return nil, errMempoolFull
		}
		// Verify the nonce again under lock.
		// Here we have a race condition between app execution and evmNonces updates.
		// This is fine, as our pending nonce tracking is always consistent,
		// and converges to correct state if sender's txs are submitted sequentially in nonce order.
		if r, ok := m.evmNonces[addr]; ok {
			if r.next != resp.EVMNonce {
				return nil, fmt.Errorf("%w: got %v, want %v", errBadNonce, resp.EVMNonce, r.next)
			}
			r.next += 1
		} else {
			m.evmNonces[addr] = &nonceRange{m.nextNonceRangeID, resp.EVMNonce, resp.EVMNonce + 1}
			m.nextNonceRangeID += 1
		}
		// If any limit would be exceeded, then construct a payload.
		// Note that we use subtraction in a way avoiding arithmetic overflows.
		ok := s.cfg.maxTxsPerBlock()-uint64(len(m.nextBlock.txs)) >= 1
		ok = ok && types.MaxTxsBytesPerBlock-m.nextBlock.sizeBytes >= uint64(len(tx))
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
		b.gasEstimated += utils.Clamp[uint64](gasEstimated)
		b.gasWanted += utils.Clamp[uint64](resp.GasWanted)
		b.sizeBytes += uint64(len(tx))
		b.rawTxs = append(b.rawTxs, tx)
		p := &pendingTx{
			raw:          tx,
			addr:         addr,
			hash:         resp.EVMHash,
			nonceRangeID: m.evmNonces[addr].id,
		}
		b.txs = append(b.txs, p)
		m.evmTxs[resp.EVMHash] = p
	}
	return resp.ResponseCheckTx, nil
}
