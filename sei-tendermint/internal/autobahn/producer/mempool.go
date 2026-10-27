package producer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
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

// errPendingFull is returned when Config.MaxPendingInserts inserts are already pending. It
// matches errMempoolFull under errors.Is so callers treat both as mempool-full.
var errPendingFull = fmt.Errorf("%w: too many pending inserts", errMempoolFull)

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
	// closed is the session's closed flag, readable without the lock.
	closed *atomic.Bool
	// admitMu guards admitQueue and admitting. It is held only to add, take or hand off tickets.
	admitMu sync.Mutex
	// admitQueue holds checked InsertTx calls waiting for the next admission batch, in arrival order.
	admitQueue []*insertTicket
	// admitQueued mirrors len(admitQueue) for readers that do not hold admitMu.
	admitQueued atomic.Uint64
	// admitting is set while an InsertTx call holds the turn to run the next admission batch.
	admitting bool
}

// ticketState is where an InsertTx call stands in admission.
type ticketState uint8

const (
	ticketQueued ticketState = iota
	// ticketLeading gives the ticket's InsertTx call the turn to run the next admission batch.
	ticketLeading
	ticketFinished
)

// insertTicket is one blocked InsertTx call in the admission queue, holding the checked tx it
// appends once admitted. admitted is signalled after the tx was appended or rejected, or when
// the mempool is closed; err is then the outcome. done and err are guarded by the mempool lock.
type insertTicket struct {
	admitted utils.AtomicSend[ticketState]
	done     bool
	err      error
	// cancelled is set when the InsertTx call gave up before admission. Guarded by the mempool lock.
	cancelled bool

	tx           tmtypes.Tx
	resp         *abci.ResponseCheckTxV2
	gasWanted    uint64
	gasEstimated uint64
	appNonce     utils.Option[uint64]
	first        types.BlockNumber
	// queued is when the InsertTx call joined the session's admission queue.
	queued time.Time
}

func newInsertTicket() *insertTicket {
	return &insertTicket{admitted: utils.NewAtomicSend(ticketQueued)}
}

// finish records the ticket's outcome and wakes its InsertTx call. Must be called with the
// mempool locked.
func (t *insertTicket) finish(err error) {
	t.done = true
	t.err = err
	t.admitted.Store(ticketFinished)
}

// wait blocks until the ticket is finished or handed the admission turn, and returns which.
func (t *insertTicket) wait(ctx context.Context) (ticketState, error) {
	return t.admitted.Wait(ctx, func(state ticketState) bool { return state != ticketQueued })
}

// mempoolInner is the lock-protected session state. The Watch value is fixed for
// the session; closed ends the session without replacing that pointer.
type mempoolInner struct {
	closed bool
	// closedFlag mirrors closed for readers that do not hold the lock.
	closedFlag atomic.Bool
	capacity   uint64
	lane       types.LaneID
	first      types.BlockNumber
	next       types.BlockNumber
	blocks     map[types.BlockNumber]*blockSpec
	nextBlock  *blockSpec
	evmNonces  map[common.Address]uint64
	evmTxs     map[common.Hash]tmtypes.Tx
	// waiters is the FIFO admission queue of blocked InsertTx calls. Whoever frees capacity
	// appends queued txs in order until the mempool is full again.
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

// close ends the session and fails every blocked InsertTx call with ErrNotProducing.
func (mp *mempool) close(m *mempoolInner, ctrl *utils.WatchCtrl) {
	m.closed = true
	m.closedFlag.Store(true)
	for _, t := range m.waiters {
		t.finish(ErrNotProducing)
	}
	m.waiters = nil
	mp.pendingInserts.Store(0)
	ctrl.Updated()
}

func (mp *mempool) enqueue(m *mempoolInner, t *insertTicket) {
	m.waiters = append(m.waiters, t)
	mp.pendingInserts.Store(uint64(len(m.waiters)))
}

// dequeue removes t from the admission queue.
func (mp *mempool) dequeue(m *mempoolInner, t *insertTicket) {
	if i := slices.Index(m.waiters, t); i >= 0 {
		m.waiters = slices.Delete(m.waiters, i, i+1)
		mp.pendingInserts.Store(uint64(len(m.waiters)))
	}
}

// drainWaiters appends queued txs in arrival order until the mempool is full or the queue is
// empty, finishing each ticket with its outcome. Must be called with the mempool locked.
func (s *State) drainWaiters(mp *mempool, m *mempoolInner, ctrl *utils.WatchCtrl) {
	admitted := 0
	for admitted < len(m.waiters) && !m.closed && !m.IsFull() {
		t := m.waiters[admitted]
		admitted++
		t.finish(s.appendTx(m, ctrl, t.tx, t.resp, t.gasWanted, t.gasEstimated, t.appNonce, t.first))
	}
	if admitted == 0 {
		return
	}
	clear(m.waiters[:admitted])
	m.waiters = m.waiters[admitted:]
	mp.pendingInserts.Store(uint64(len(m.waiters)))
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
			var unsettled []common.Address
			var wantNonces []uint64
			for addr, wantNonce := range b.evmNonces {
				if wantNonce == m.evmNonces[addr] {
					// Happy path: all account's txs got executed.
					delete(m.evmNonces, addr)
				} else {
					unsettled = append(unsettled, addr)
					wantNonces = append(wantNonces, wantNonce)
				}
			}
			if len(unsettled) == 0 {
				continue
			}
			for i, gotNonce := range s.app.EvmNonces(unsettled) {
				if gotNonce < wantNonces[i] {
					// Some txs have not been executed - reset account tracking.
					// NOTE: app execution is not synchronized with mempool, so nonce could have already
					// proceeded past wantNonce and that is expected.
					addr := unsettled[i]
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
		s.drainWaiters(mp, m, ctrl)
	}
}

// TryInsertTx inserts tx to the mempool. Returns errMempoolFull if the mempool is full or
// Config.MaxPendingInserts TryInsertTx calls are already in flight.
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

// preReadEvmNonce reads the app nonce of addr outside the mempool lock, returning None
// when the mempool already tracks addr. It also returns the lane's first block at the
// time of the check, which insertTx uses to detect prunes racing the read.
func (s *State) preReadEvmNonce(mp *mempool, addr common.Address) (utils.Option[uint64], types.BlockNumber, error) {
	var first types.BlockNumber
	for m := range mp.inner.Lock() {
		if m.closed {
			return utils.None[uint64](), 0, ErrNotProducing
		}
		first = m.first
		if _, tracked := m.evmNonces[addr]; tracked {
			return utils.None[uint64](), first, nil
		}
	}
	return utils.Some(s.evmNonce(addr)), first, nil
}

// poolCanAdmit returns nil when the pool would admit or queue an insert without a ticket right
// now, and otherwise the error the admission loop would return, so such an insert skips CheckTx.
// The admission loop rechecks after CheckTx.
func (s *State) poolCanAdmit(mp *mempool, waitIfFull bool) error {
	if waitIfFull {
		// Read without the mempool lock: under overload every insert passes here, and contending
		// for the lock starves the producer that frees capacity.
		switch {
		case mp.closed.Load():
			return ErrNotProducing
		case mp.pendingInserts.Load()+mp.admitQueued.Load() >= s.cfg.maxPendingInserts():
			return errPendingFull
		}
		return nil
	}
	for m := range mp.inner.Lock() {
		switch {
		case m.closed:
			return ErrNotProducing
		case !waitIfFull && m.IsFull():
			return errMempoolFull
		case waitIfFull && uint64(len(m.waiters)) >= s.cfg.maxPendingInserts():
			return errPendingFull
		}
	}
	return nil
}

// checkTx runs the app CheckTx for tx, holding one of cfg.MaxConcurrentCheckTx permits
// for the duration of the call. Waiting for a permit is cancelled with ctx.
func (s *State) checkTx(ctx context.Context, tx tmtypes.Tx) (*abci.ResponseCheckTxV2, error) {
	release, err := s.acquireCheckTxPermit(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	defer metrics.PhaseCheckTx.Enter()()
	start := time.Now()
	defer func() { metrics.ObserveCheckTx(time.Since(start)) }()
	return s.app.CheckTxSafe(ctx, &abci.RequestCheckTxV2{Tx: tx})
}

// acquireCheckTxPermit takes one CheckTx permit, recording the wait as its own insert phase.
func (s *State) acquireCheckTxPermit(ctx context.Context) (func(), error) {
	defer metrics.PhaseCheckTxWait.Enter()()
	start := time.Now()
	defer func() { metrics.ObserveCheckTxWait(time.Since(start)) }()
	return s.checkTxSem.Acquire(ctx)
}

// evmNonce reads the executed nonce of addr from the app.
func (s *State) evmNonce(addr common.Address) uint64 {
	start := time.Now()
	defer func() { metrics.ObserveNonceLookup(time.Since(start)) }()
	return s.app.EvmNonce(addr)
}

// waitForCapacity blocks until the ticket is finished or handed the admission turn, returning which
// and the time spent waiting.
func (t *insertTicket) waitForCapacity(ctx context.Context) (ticketState, time.Duration, error) {
	defer metrics.PhaseCapacityWait.Enter()()
	start := time.Now()
	state, err := t.wait(ctx)
	return state, time.Since(start), err
}

// admitQueued hands a checked tx to the session's admission queue and waits for its outcome. A call
// handed the admission turn runs one batch, which includes its own tx, before waiting on.
func (s *State) admitQueued(ctx context.Context, mp *mempool, tx tmtypes.Tx, resp *abci.ResponseCheckTxV2, gasWanted, gasEstimated uint64) (*abci.ResponseCheckTx, error) {
	ticket := newInsertTicket()
	ticket.tx, ticket.resp = tx, resp
	ticket.gasWanted, ticket.gasEstimated = gasWanted, gasEstimated
	ticket.queued = time.Now()
	if mp.queueForAdmission(ticket) {
		s.admitNextBatch(mp)
	}
	var waited time.Duration
	defer func() { metrics.ObserveCapacityWait(waited) }()
	for {
		state, d, err := ticket.waitForCapacity(ctx)
		waited += d
		if err != nil {
			return s.abandonTicket(mp, ticket, err)
		}
		if state == ticketFinished {
			break
		}
		// Only the call holding the turn writes a ticket still in the queue.
		ticket.admitted.Store(ticketQueued)
		s.admitNextBatch(mp)
	}
	// finish publishes err before it marks the ticket finished, which the wait observed.
	if ticket.err != nil {
		return nil, ticket.err
	}
	return resp.ResponseCheckTx, nil
}

// queueForAdmission adds ticket to the admission queue and reports whether its InsertTx call takes the
// turn to run the next batch.
func (mp *mempool) queueForAdmission(ticket *insertTicket) bool {
	mp.admitMu.Lock()
	defer mp.admitMu.Unlock()
	mp.admitQueue = append(mp.admitQueue, ticket)
	mp.admitQueued.Store(uint64(len(mp.admitQueue)))
	lead := !mp.admitting
	mp.admitting = true
	return lead
}

// admitNextBatch admits every queued insert as one batch, then passes the turn on. Only the InsertTx
// call holding the turn may call it.
func (s *State) admitNextBatch(mp *mempool) {
	mp.admitMu.Lock()
	batch := mp.admitQueue
	mp.admitQueue = nil
	mp.admitQueued.Store(0)
	mp.admitMu.Unlock()
	s.admitBatch(mp, batch)
	mp.handOffAdmission()
}

// handOffAdmission gives the admission turn to the oldest queued insert, or ends admission when none is
// queued.
func (mp *mempool) handOffAdmission() {
	mp.admitMu.Lock()
	defer mp.admitMu.Unlock()
	if len(mp.admitQueue) == 0 {
		mp.admitting = false
		return
	}
	mp.admitQueue[0].admitted.Store(ticketLeading)
}

// abandonTicket withdraws the ticket of an InsertTx call that stopped waiting. A ticket holding the
// admission turn still runs its batch, so the inserts behind it are not stranded, and a tx appended
// meanwhile is reported as admitted.
func (s *State) abandonTicket(mp *mempool, ticket *insertTicket, waitErr error) (*abci.ResponseCheckTx, error) {
	mp.admitMu.Lock()
	if i := slices.Index(mp.admitQueue, ticket); i >= 0 && ticket.admitted.Load() == ticketQueued {
		mp.admitQueue = slices.Delete(mp.admitQueue, i, i+1)
		mp.admitQueued.Store(uint64(len(mp.admitQueue)))
		mp.admitMu.Unlock()
		return nil, waitErr
	}
	leading := ticket.admitted.Load() == ticketLeading
	mp.admitMu.Unlock()
	if leading {
		ticket.admitted.Store(ticketQueued)
		s.admitNextBatch(mp)
	}
	for m, ctrl := range mp.inner.Lock() {
		if !ticket.done {
			// Cancelled before admission: leave the queue so the calls behind it are not held up.
			ticket.cancelled = true
			mp.dequeue(m, ticket)
			s.drainWaiters(mp, m, ctrl)
			return nil, waitErr
		}
	}
	if ticket.err != nil {
		return nil, ticket.err
	}
	return ticket.resp.ResponseCheckTx, nil
}

// admitBatch reads the nonces of the batch's untracked senders outside the lock, then appends or
// queues each insert in order under one lock. Admission is FIFO: a large tx is never passed over
// in favor of smaller ones behind it.
func (s *State) admitBatch(mp *mempool, batch []*insertTicket) {
	defer metrics.PhaseAdmit.Enter()()
	defer func() {
		now := time.Now()
		for _, ticket := range batch {
			metrics.ObserveAdmit(now.Sub(ticket.queued))
		}
	}()
	for attempt := 1; ; attempt++ {
		untracked, first := s.readUntrackedNonces(mp, batch)
		for m, ctrl := range mp.inner.Lock() {
			// A prune since the read can advance a sender's app nonce, and appendTx would then read it
			// again under this lock for each such sender, so the read is redone outside the lock instead.
			if m.first != first && attempt < admissionReadAttempts {
				break
			}
			for _, ticket := range batch {
				if ticket.cancelled {
					continue
				}
				if ticket.resp.IsEVM {
					ticket.appNonce, ticket.first = untracked[ticket.resp.EVMSenderAddress], first
				}
				switch {
				case m.closed:
					ticket.finish(ErrNotProducing)
				case !m.IsFull() && len(m.waiters) == 0:
					ticket.finish(s.appendTx(m, ctrl, ticket.tx, ticket.resp, ticket.gasWanted, ticket.gasEstimated, ticket.appNonce, ticket.first))
				case uint64(len(m.waiters)) >= s.cfg.maxPendingInserts():
					ticket.finish(errPendingFull)
				default:
					mp.enqueue(m, ticket)
				}
			}
			s.drainWaiters(mp, m, ctrl)
			return
		}
	}
}

// admissionReadAttempts is how many times admitBatch reads nonces outside the lock before it appends
// with whatever a prune left stale.
const admissionReadAttempts = 3

// readUntrackedNonces reads, outside the mempool lock, the app nonce of every EVM sender in batch the
// mempool does not track, and returns them with the lane's first block at the time of the check.
func (s *State) readUntrackedNonces(mp *mempool, batch []*insertTicket) (map[common.Address]utils.Option[uint64], types.BlockNumber) {
	untracked := map[common.Address]utils.Option[uint64]{}
	var addrs []common.Address
	var first types.BlockNumber
	for m := range mp.inner.Lock() {
		first = m.first
		for _, ticket := range batch {
			if !ticket.resp.IsEVM {
				continue
			}
			addr := ticket.resp.EVMSenderAddress
			if _, tracked := m.evmNonces[addr]; !tracked {
				if _, seen := untracked[addr]; !seen {
					untracked[addr] = utils.None[uint64]()
					addrs = append(addrs, addr)
				}
			}
		}
	}
	if len(addrs) > 0 {
		start := time.Now()
		for i, nonce := range s.app.EvmNonces(addrs) {
			untracked[addrs[i]] = utils.Some(nonce)
		}
		metrics.ObserveNonceLookup(time.Since(start))
	}
	return untracked, first
}

// insertResult classifies an insert outcome for the inserts metric.
func insertResult(resp *abci.ResponseCheckTx, err error) metrics.Result {
	switch {
	case errors.Is(err, errTooLarge):
		return metrics.ResultTooLarge
	case errors.Is(err, errPendingFull):
		return metrics.ResultPendingFull
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
	resp, err := s.boundedInsertTx(ctx, tx, waitIfFull)
	insertResult(resp, err).Observe()
	return resp, err
}

// boundedInsertTx runs doInsertTx, holding one of cfg.MaxPendingInserts in-flight slots for
// the duration of a TryInsertTx call. Returns errPendingFull when no slot is free.
func (s *State) boundedInsertTx(ctx context.Context, tx tmtypes.Tx, waitIfFull bool) (*abci.ResponseCheckTx, error) {
	if waitIfFull {
		return s.doInsertTx(ctx, tx, waitIfFull)
	}
	release, ok := s.tryInsertSem.TryAcquire()
	if !ok {
		return nil, errPendingFull
	}
	defer release()
	return s.doInsertTx(ctx, tx, waitIfFull)
}

func (s *State) doInsertTx(ctx context.Context, tx tmtypes.Tx, waitIfFull bool) (*abci.ResponseCheckTx, error) {
	if uint64(len(tx)) > types.MaxTxsBytesPerBlock {
		return nil, errTooLarge
	}
	// Reject / wait for a produce session before CheckTxSafe — IsFull and closed
	// are checked again after, since they can change while CheckTx runs.
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
	if err := s.poolCanAdmit(mp, waitIfFull); err != nil {
		return nil, err
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

	if waitIfFull {
		return s.admitQueued(ctx, mp, tx, resp, gasWanted, gasEstimated)
	}
	appNonce := utils.None[uint64]()
	var first types.BlockNumber
	if resp.IsEVM {
		appNonce, first, err = s.preReadEvmNonce(mp, resp.EVMSenderAddress)
		if err != nil {
			return nil, err
		}
	}

	admitStart := time.Now()
	defer func() { metrics.ObserveAdmit(time.Since(admitStart)) }()
	defer metrics.PhaseAdmit.Enter()()
	for m, ctrl := range mp.inner.Lock() {
		if m.closed {
			return nil, ErrNotProducing
		}
		s.drainWaiters(mp, m, ctrl)
		// A call that may not wait never joins the queue, and never jumps ahead of it.
		if m.IsFull() || len(m.waiters) > 0 {
			return nil, errMempoolFull
		}
		if err := s.appendTx(m, ctrl, tx, resp, gasWanted, gasEstimated, appNonce, first); err != nil {
			return nil, err
		}
	}
	return resp.ResponseCheckTx, nil
}

// appendTx adds an admitted tx to the next lane block, sealing the current one first when the
// tx would exceed one of its limits. appNonce is the sender's app nonce pre-read outside the
// lock, valid only while m.first is unchanged since the read; see preReadEvmNonce.
// Must be called with the mempool locked and not full.
func (s *State) appendTx(m *mempoolInner, ctrl *utils.WatchCtrl, tx tmtypes.Tx, resp *abci.ResponseCheckTxV2, gasWanted, gasEstimated uint64, appNonce utils.Option[uint64], first types.BlockNumber) error {
	if resp.IsEVM {
		addr := resp.EVMSenderAddress
		nonce, ok := m.evmNonces[addr]
		if !ok {
			// The tracked entry, when present, is authoritative: it covers txs already
			// sequenced but not yet executed. The pre-read app nonce is used only when
			// there is no entry and no block was pruned since it was taken (m.first
			// unchanged), since pruning may delete this sender's entry and advance the
			// app nonce.
			if pre, ok := appNonce.Get(); ok && m.first == first {
				nonce = pre
			} else {
				nonce = s.evmNonce(addr)
			}
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
