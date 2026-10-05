package producer

import (
	"cmp"
	"container/heap"
	"context"
	"iter"
	"math"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// unqueuedSeq is the seq of a call that is not queued yet, so it loses every tie to a queued one.
const unqueuedSeq = math.MaxUint64

const (
	// fitContiguous keeps the sender's queued nonces contiguous: the sender has nothing queued,
	// or the nonce directly follows its last queued call or directly precedes its first.
	fitContiguous nonceFit = iota
	// fitGap leaves a gap in the sender's queued nonces.
	fitGap
	// fitDuplicate repeats a nonce already queued for the sender.
	fitDuplicate
)

// rank orders blocked InsertTx calls for admission: a sender of a shard this validator owns
// first, then higher priority, then earlier arrival.
type rank struct {
	owned    bool
	priority int64
	seq      uint64
}

// outranks reports whether r is admitted before o.
func (r rank) outranks(o rank) bool {
	if r.owned != o.owned {
		return r.owned
	}
	if r.priority != o.priority {
		return r.priority > o.priority
	}
	return r.seq < o.seq
}

// lower returns whichever of r and o is admitted later.
func (r rank) lower(o rank) rank {
	if r.outranks(o) {
		return o
	}
	return r
}

// insertTicket is the place of one blocked InsertTx call in the admission queue.
// admitted is signalled when the ticket is the best eligible call and the mempool has
// capacity, when the ticket is evicted, or when the mempool is closed.
type insertTicket struct {
	admitted utils.AtomicSend[bool]
	rank     rank
	// sender and nonce order the calls of one EVM sender; None for a non-EVM tx.
	sender utils.Option[common.Address]
	nonce  uint64

	// The fields below are guarded by the mempool lock.
	queued   bool
	evicted  bool
	bestIdx  int
	worstIdx int
}

func newInsertTicket(r rank, sender utils.Option[common.Address], nonce uint64) *insertTicket {
	return &insertTicket{
		admitted: utils.NewAtomicSend(false),
		rank:     r,
		sender:   sender,
		nonce:    nonce,
		bestIdx:  -1,
		worstIdx: -1,
	}
}

func (t *insertTicket) wait(ctx context.Context) error {
	_, err := t.admitted.Wait(ctx, func(admitted bool) bool { return admitted })
	return err
}

// nonceOrder orders the calls of one sender: lower nonce first, then earlier arrival.
func nonceOrder(a, b *insertTicket) int {
	return cmp.Or(cmp.Compare(a.nonce, b.nonce), cmp.Compare(a.rank.seq, b.rank.seq))
}

// ticketHeap is a heap.Interface over tickets with the first under before on top. pos selects
// the ticket field that holds its index, so a ticket can sit in two heaps and leave either in O(log n).
type ticketHeap struct {
	items  []*insertTicket
	before func(a, b *insertTicket) bool
	pos    func(t *insertTicket) *int
}

func (h *ticketHeap) Len() int           { return len(h.items) }
func (h *ticketHeap) Less(i, j int) bool { return h.before(h.items[i], h.items[j]) }

func (h *ticketHeap) Swap(i, j int) {
	h.items[i], h.items[j] = h.items[j], h.items[i]
	*h.pos(h.items[i]) = i
	*h.pos(h.items[j]) = j
}

func (h *ticketHeap) Push(x any) {
	t := x.(*insertTicket)
	*h.pos(t) = len(h.items)
	h.items = append(h.items, t)
}

func (h *ticketHeap) Pop() any {
	n := len(h.items) - 1
	t := h.items[n]
	h.items[n] = nil
	h.items = h.items[:n]
	*h.pos(t) = -1
	return t
}

func (h *ticketHeap) top() utils.Option[*insertTicket] {
	if len(h.items) == 0 {
		return utils.None[*insertTicket]()
	}
	return utils.Some(h.items[0])
}

// admissionQueue holds the blocked InsertTx calls. A call is eligible unless an earlier-nonce
// call of the same EVM sender is queued; eligible calls are indexed best-first and worst-first.
type admissionQueue struct {
	best  ticketHeap
	worst ticketHeap
	// senders holds the queued calls of each EVM sender in nonceOrder; only the first is eligible.
	senders map[common.Address][]*insertTicket
	len     uint64
	nextSeq uint64
}

func newAdmissionQueue() *admissionQueue {
	return &admissionQueue{
		best: ticketHeap{
			before: func(a, b *insertTicket) bool { return a.rank.outranks(b.rank) },
			pos:    func(t *insertTicket) *int { return &t.bestIdx },
		},
		worst: ticketHeap{
			before: func(a, b *insertTicket) bool { return b.rank.outranks(a.rank) },
			pos:    func(t *insertTicket) *int { return &t.worstIdx },
		},
		senders: map[common.Address][]*insertTicket{},
	}
}

// Len returns the number of queued calls, eligible or not.
func (q *admissionQueue) Len() uint64 { return q.len }

// Best returns the eligible call to admit next.
func (q *admissionQueue) Best() utils.Option[*insertTicket] { return q.best.top() }

// Worst returns the lowest-ranked eligible call.
func (q *admissionQueue) Worst() utils.Option[*insertTicket] { return q.worst.top() }

// All yields every queued call.
func (q *admissionQueue) All() iter.Seq[*insertTicket] {
	return func(yield func(*insertTicket) bool) {
		for _, t := range q.best.items {
			if !t.sender.IsPresent() && !yield(t) {
				return
			}
		}
		for _, calls := range q.senders {
			for _, t := range calls {
				if !yield(t) {
					return
				}
			}
		}
	}
}

// predecessor returns the eligible call of t's sender when t would queue behind it.
func (q *admissionQueue) predecessor(t *insertTicket) utils.Option[*insertTicket] {
	addr, ok := t.sender.Get()
	if !ok {
		return utils.None[*insertTicket]()
	}
	calls := q.senders[addr]
	if len(calls) == 0 || calls[0] == t || nonceOrder(calls[0], t) > 0 {
		return utils.None[*insertTicket]()
	}
	return utils.Some(calls[0])
}

// effectiveRank is the rank t competes at: a call behind its sender's eligible call ranks no
// higher than that call.
func (q *admissionQueue) effectiveRank(t *insertTicket) rank {
	if p, ok := q.predecessor(t).Get(); ok {
		return t.rank.lower(p.rank)
	}
	return t.rank
}

// nonceFit is how the nonce of an unqueued call relates to the queued calls of its sender.
type nonceFit int

// Fit returns how the nonce of unqueued t relates to the queued calls of its sender. A call
// without a sender is contiguous.
func (q *admissionQueue) Fit(t *insertTicket) nonceFit {
	addr, ok := t.sender.Get()
	if !ok {
		return fitContiguous
	}
	calls := q.senders[addr]
	if len(calls) == 0 {
		return fitContiguous
	}
	if _, found := slices.BinarySearchFunc(calls, t.nonce, func(c *insertTicket, nonce uint64) int {
		return cmp.Compare(c.nonce, nonce)
	}); found {
		return fitDuplicate
	}
	first, last := calls[0].nonce, calls[len(calls)-1].nonce
	if (last < math.MaxUint64 && t.nonce == last+1) || (first > 0 && t.nonce == first-1) {
		return fitContiguous
	}
	return fitGap
}

// AdmitsNext reports whether t is the next call to admit: the best eligible call when queued,
// otherwise ahead of every queued call.
func (q *admissionQueue) AdmitsNext(t *insertTicket) bool {
	best, ok := q.Best().Get()
	if t.queued {
		return best == t
	}
	return !ok || (!q.predecessor(t).IsPresent() && t.rank.outranks(best.rank))
}

// VictimFor returns the queued call that unqueued t evicts from a full queue: the last call of
// the lowest-ranked eligible call's sender, so no sender is left with a nonce gap. None when t
// does not keep its sender's queued nonces contiguous, or does not outrank that eligible call.
func (q *admissionQueue) VictimFor(t *insertTicket) utils.Option[*insertTicket] {
	if q.Fit(t) != fitContiguous {
		return utils.None[*insertTicket]()
	}
	w, ok := q.Worst().Get()
	if !ok || !q.effectiveRank(t).outranks(w.rank) {
		return utils.None[*insertTicket]()
	}
	addr, ok := w.sender.Get()
	if !ok {
		return utils.Some(w)
	}
	calls := q.senders[addr]
	return utils.Some(calls[len(calls)-1])
}

// Push queues t behind every call already queued at its rank.
func (q *admissionQueue) Push(t *insertTicket) {
	t.rank.seq = q.nextSeq
	q.nextSeq += 1
	t.queued = true
	q.len += 1
	addr, ok := t.sender.Get()
	if !ok {
		q.makeEligible(t)
		return
	}
	calls := q.senders[addr]
	i, _ := slices.BinarySearchFunc(calls, t, nonceOrder)
	calls = slices.Insert(calls, i, t)
	q.senders[addr] = calls
	if i == 0 {
		if len(calls) > 1 {
			q.makeIneligible(calls[1])
		}
		q.makeEligible(t)
	}
}

// Remove takes t out of the queue, making the next call of its sender eligible. Reports
// whether t was queued.
func (q *admissionQueue) Remove(t *insertTicket) bool {
	if !t.queued {
		return false
	}
	t.queued = false
	q.len -= 1
	addr, ok := t.sender.Get()
	if !ok {
		q.makeIneligible(t)
		return true
	}
	calls := q.senders[addr]
	i, _ := slices.BinarySearchFunc(calls, t, nonceOrder)
	calls = slices.Delete(calls, i, i+1)
	if i == 0 {
		q.makeIneligible(t)
	}
	if len(calls) == 0 {
		delete(q.senders, addr)
		return true
	}
	q.senders[addr] = calls
	if i == 0 {
		q.makeEligible(calls[0])
	}
	return true
}

func (q *admissionQueue) makeEligible(t *insertTicket) {
	heap.Push(&q.best, t)
	heap.Push(&q.worst, t)
}

func (q *admissionQueue) makeIneligible(t *insertTicket) {
	heap.Remove(&q.best, t.bestIdx)
	heap.Remove(&q.worst, t.worstIdx)
}
