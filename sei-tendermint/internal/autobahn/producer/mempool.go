package producer

import (
	"context"
	"errors"
	"fmt"
	"slices"
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

type blockSpec struct {
	gasEstimated uint64
	gasWanted    uint64
	sizeBytes    uint64
	txs          [][]byte
	evmHashes    []common.Hash
	// nonces of accounts which are expected to be bumped by this block.
	// They are checked against the app state after the block is executed.
	evmNonces map[common.Address]uint64
}

// mempool is one produce session. State.mempool publishes it; nil means idle.
type mempool struct {
	inner utils.Watch[*mempoolInner]
	// pendingInserts is the number of InsertTx calls blocked in the admission queue.
	pendingInserts utils.AtomicSend[uint64]
}

// insertTicket is the place of one blocked InsertTx call in the admission queue.
// admitted is signalled when the ticket is at the head of the queue and the
// mempool has capacity, or when the mempool is closed.
type insertTicket struct {
	admitted utils.AtomicSend[bool]
}

func newInsertTicket() *insertTicket {
	return &insertTicket{admitted: utils.NewAtomicSend(false)}
}

func (t *insertTicket) wait(ctx context.Context) error {
	_, err := t.admitted.Wait(ctx, func(admitted bool) bool { return admitted })
	return err
}

// mempoolInner is the lock-protected session state. The Watch value is fixed for
// the session; closed ends the session without replacing that pointer.
type mempoolInner struct {
	closed    bool
	capacity  uint64
	lane      types.LaneID
	first     types.BlockNumber
	next      types.BlockNumber
	blocks    map[types.BlockNumber]*blockSpec
	nextBlock *blockSpec
	evmNonces map[common.Address]uint64
	evmTxs    map[common.Hash]tmtypes.Tx
	// waiters is the FIFO admission queue of blocked InsertTx calls; only the head is
	// ever signalled, so an update costs O(1) regardless of the queue length.
	waiters []*insertTicket
}

func newMempoolInner(capacity uint64, lane types.LaneID, n types.BlockNumber) *mempoolInner {
	return &mempoolInner{
		capacity:  capacity,
		lane:      lane,
		first:     n,
		next:      n,
		blocks:    map[types.BlockNumber]*blockSpec{},
		nextBlock: &blockSpec{evmNonces: map[common.Address]uint64{}},
		evmNonces: map[common.Address]uint64{},
		evmTxs:    map[common.Hash]tmtypes.Tx{},
	}
}

// close ends the session and releases every blocked InsertTx call.
func (m *mempoolInner) close(ctrl *utils.WatchCtrl) {
	m.closed = true
	for _, t := range m.waiters {
		t.admitted.Store(true)
	}
	ctrl.Updated()
}

// isHead reports whether ticket is the next call to be admitted: a call without a
// ticket is admitted only when nobody is queued ahead of it.
func (m *mempoolInner) isHead(ticket utils.Option[*insertTicket]) bool {
	t, ok := ticket.Get()
	if !ok {
		return len(m.waiters) == 0
	}
	return m.waiters[0] == t
}

// signalHead wakes the oldest blocked InsertTx call if the mempool has capacity.
func (m *mempoolInner) signalHead() {
	if len(m.waiters) > 0 && !m.IsFull() {
		m.waiters[0].admitted.Store(true)
	}
}

func (mp *mempool) enqueue(m *mempoolInner) *insertTicket {
	t := newInsertTicket()
	m.waiters = append(m.waiters, t)
	mp.pendingInserts.Store(uint64(len(m.waiters)))
	return t
}

// dequeue removes t from the admission queue and passes the turn to the next waiter.
func (mp *mempool) dequeue(m *mempoolInner, t *insertTicket) {
	if i := slices.Index(m.waiters, t); i >= 0 {
		m.waiters = slices.Delete(m.waiters, i, i+1)
		mp.pendingInserts.Store(uint64(len(m.waiters)))
	}
	m.signalHead()
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
	m.nextBlock = &blockSpec{
		evmNonces: map[common.Address]uint64{},
	}
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
		return m.nextBlock.txs
	}
	panic("unreachable")
}

func (s *State) EvmNextPendingNonce(addr common.Address) uint64 {
	mp, ok := s.mempool.Load().Get()
	if !ok {
		return s.app.EvmNonce(addr)
	}
	for m := range mp.inner.Lock() {
		if !m.closed {
			if nonce, ok := m.evmNonces[addr]; ok {
				return nonce
			}
		}
	}
	return s.app.EvmNonce(addr)
}

func (s *State) EvmTxByHash(hash common.Hash) (tmtypes.Tx, bool) {
	mp, ok := s.mempool.Load().Get()
	if !ok {
		return nil, false
	}
	for m := range mp.inner.Lock() {
		if m.closed {
			return nil, false
		}
		tx, ok := m.evmTxs[hash]
		return tx, ok
	}
	panic("unreachable")
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
			for _, hash := range b.evmHashes {
				delete(m.evmTxs, hash)
			}
			for addr, wantNonce := range b.evmNonces {
				if wantNonce == m.evmNonces[addr] {
					// Happy path: all account's txs got executed.
					delete(m.evmNonces, addr)
				} else if gotNonce := s.app.EvmNonce(addr); gotNonce < wantNonce {
					// Some txs have not been executed - reset account tracking.
					// NOTE: app execution is not synchronized with mempool, so nonce could have already
					// proceeded past wantNonce and that is expected.
					delete(m.evmNonces, addr)
					delete(m.nextBlock.evmNonces, addr)
					for _, x := range m.blocks {
						delete(x.evmNonces, addr)
					}
				}
			}
		}
		// n > m.next shouldn't really happen,
		// because local mempool is the only source of local lane blocks,
		// but we handle it gracefully anyway.
		m.next = max(m.next, n)
		m.signalHead()
	}
}

// TryInsertTx inserts tx to the mempool. Returns error if mempool is full.
func (s *State) TryInsertTx(ctx context.Context, tx tmtypes.Tx) (*abci.ResponseCheckTx, error) {
	return s.insertTx(ctx, tx, false)
}

// InsertTx inserts tx to the mempool. Blocks if mempool is full; blocked InsertTx calls are
// admitted in arrival order relative to each other, but TryInsertTx calls do not queue and may
// take freed capacity ahead of them. Returns errMempoolFull once Config.MaxPendingInserts calls are blocked.
// The blocked calls are effectively the "unsequenced" part of the mempool.
// After InsertTx returns, the sequence is already scheduled to be included in a lane.
// TODO(gprusak): we might need some prioritization mechanism in case our node can handle more InsertTx calls/s
// than the lane throughput.
func (s *State) InsertTx(ctx context.Context, tx tmtypes.Tx) (*abci.ResponseCheckTx, error) {
	return s.insertTx(ctx, tx, true)
}

// getMempool waits until a produce session is published so inserts racing the
// start of production are admitted. Leave (LocalLane gone) ends the wait with
// ErrNotProducing — including when clearMempool publishes None after the session ends.
func (s *State) getMempool(ctx context.Context) (*mempool, error) {
	if mp, ok := s.mempool.Load().Get(); ok {
		return mp, nil
	}
	if _, ok := s.consensus.Avail().LocalLane().Get(); !ok {
		return nil, ErrNotProducing
	}
	opt, err := s.mempool.Wait(ctx, func(opt utils.Option[*mempool]) bool {
		if opt.IsPresent() {
			return true
		}
		_, ok := s.consensus.Avail().LocalLane().Get()
		return !ok
	})
	if err != nil {
		return nil, err
	}
	mp, ok := opt.Get()
	if !ok {
		return nil, ErrNotProducing
	}
	return mp, nil
}

// checkTx runs the app CheckTx for tx.
func (s *State) checkTx(ctx context.Context, tx tmtypes.Tx) (*abci.ResponseCheckTxV2, error) {
	defer metrics.PhaseCheckTx.Enter()()
	start := time.Now()
	defer func() { metrics.ObserveCheckTx(time.Since(start)) }()
	return s.app.CheckTxSafe(ctx, &abci.RequestCheckTxV2{Tx: tx})
}

// evmNonce reads the executed nonce of addr from the app.
func (s *State) evmNonce(addr common.Address) uint64 {
	start := time.Now()
	defer func() { metrics.ObserveNonceLookup(time.Since(start)) }()
	return s.app.EvmNonce(addr)
}

// waitForCapacity blocks until the ticket is signalled, returning the time spent waiting.
func (t *insertTicket) waitForCapacity(ctx context.Context) (time.Duration, error) {
	defer metrics.PhaseCapacityWait.Enter()()
	start := time.Now()
	err := t.wait(ctx)
	return time.Since(start), err
}

// insertResult classifies an insert outcome for the inserts metric.
func insertResult(resp *abci.ResponseCheckTx, err error) metrics.Result {
	switch {
	case errors.Is(err, errTooLarge):
		return metrics.ResultTooLarge
	case errors.Is(err, errMempoolFull):
		return metrics.ResultFull
	case errors.Is(err, ErrNotProducing):
		return metrics.ResultNotProducing
	case errors.Is(err, errBadNonce):
		return metrics.ResultBadNonce
	case err != nil:
		return metrics.ResultError
	case !resp.IsOK():
		return metrics.ResultRejected
	default:
		return metrics.ResultOK
	}
}

// Inserts transaction. Blocks until there is capacity in the mempool.
// NOTE: we currently don't do any tx filtering, which would prevent expensive CheckTxSafe calls.
// It has to be added after testnet launch.
func (s *State) insertTx(ctx context.Context, tx tmtypes.Tx, waitIfFull bool) (*abci.ResponseCheckTx, error) {
	resp, err := s.doInsertTx(ctx, tx, waitIfFull)
	insertResult(resp, err).Observe()
	return resp, err
}

func (s *State) doInsertTx(ctx context.Context, tx tmtypes.Tx, waitIfFull bool) (*abci.ResponseCheckTx, error) {
	if uint64(len(tx)) > types.MaxTxsBytesPerBlock {
		return nil, errTooLarge
	}
	// Reject / wait for a produce session before CheckTxSafe — IsFull and closed
	// are checked after, since they can change while CheckTx runs.
	var mp *mempool
	var err error
	if waitIfFull {
		mp, err = s.getMempool(ctx)
		if err != nil {
			return nil, err
		}
	} else {
		loaded, ok := s.mempool.Load().Get()
		if !ok {
			return nil, ErrNotProducing
		}
		mp = loaded
	}
	resp, err := s.checkTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !resp.IsOK() {
		return resp.ResponseCheckTx, nil
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

	admitStart := time.Now()
	var waited time.Duration
	var wakeups int64
	defer func() { metrics.ObserveAdmit(time.Since(admitStart) - waited) }()
	leaveAdmit := metrics.PhaseAdmit.Enter()
	defer func() { leaveAdmit() }()
	// mempool is constructed as a FIFO - we do not delay insertions of large txs (going over cap)
	// in favor of waiting for smaller txs. This simple algorithm allows us to cap
	// pending txs to size of a single block. We can refine this rule later if needed.
	// Blocked calls queue up in arrival order and only the head is woken when capacity
	// frees up, so a mempool update costs O(1) regardless of the number of waiters.
	ticket := utils.None[*insertTicket]()
	defer func() {
		if ticket.IsPresent() {
			metrics.ObserveCapacityWait(waited, wakeups)
		}
	}()
	for {
		if t, ok := ticket.Get(); ok {
			leaveAdmit()
			d, err := t.waitForCapacity(ctx)
			waited += d
			leaveAdmit = metrics.PhaseAdmit.Enter()
			if err != nil {
				for m := range mp.inner.Lock() {
					mp.dequeue(m, t)
				}
				return nil, err
			}
		}
		for m, ctrl := range mp.inner.Lock() {
			if m.closed {
				if t, ok := ticket.Get(); ok {
					mp.dequeue(m, t)
				}
				return nil, ErrNotProducing
			}
			if m.IsFull() && !waitIfFull {
				return nil, errMempoolFull
			}
			if m.IsFull() || (waitIfFull && !m.isHead(ticket)) {
				if t, ok := ticket.Get(); ok {
					// A TryInsertTx may have filled the mempool since this ticket was signalled.
					wakeups++
					t.admitted.Store(false)
				} else {
					if uint64(len(m.waiters)) >= s.cfg.maxPendingInserts() {
						return nil, errMempoolFull
					}
					ticket = utils.Some(mp.enqueue(m))
				}
				continue
			}
			err := s.appendTx(m, ctrl, tx, resp, gasWanted, gasEstimated)
			if t, ok := ticket.Get(); ok {
				mp.dequeue(m, t)
			}
			if err != nil {
				return nil, err
			}
			return resp.ResponseCheckTx, nil
		}
	}
}

// appendTx adds an admitted tx to the next lane block, sealing the current one first when the
// tx would exceed one of its limits. Must be called with the mempool locked and not full.
func (s *State) appendTx(m *mempoolInner, ctrl *utils.WatchCtrl, tx tmtypes.Tx, resp *abci.ResponseCheckTxV2, gasWanted, gasEstimated uint64) error {
	if resp.IsEVM {
		addr := resp.EVMSenderAddress
		nonce, ok := m.evmNonces[addr]
		if !ok {
			nonce = s.evmNonce(addr)
		}
		if nonce != resp.EVMNonce {
			return fmt.Errorf("%w: got %v, want %v", errBadNonce, resp.EVMNonce, nonce)
		}
		m.evmNonces[addr] = nonce + 1
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
	b.txs = append(b.txs, tx)
	if resp.IsEVM {
		addr := resp.EVMSenderAddress
		b.evmNonces[addr] = m.evmNonces[addr]
		b.evmHashes = append(b.evmHashes, resp.EVMHash)
		m.evmTxs[resp.EVMHash] = tx
	}
	return nil
}
