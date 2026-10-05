package producer

import (
	"cmp"
	"context"
	"iter"
	"math"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/btree"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// eligibleDegree is the btree degree of the eligible calls: small nodes keep inserts cheap and
// still give a depth of about 3 at the default 4096-call bound.
const eligibleDegree = 16

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
	queued  bool
	evicted bool
}

func newInsertTicket(r rank, sender utils.Option[common.Address], nonce uint64) *insertTicket {
	return &insertTicket{
		admitted: utils.NewAtomicSend(false),
		rank:     r,
		sender:   sender,
		nonce:    nonce,
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

// admissionQueue holds the blocked InsertTx calls. A call is eligible unless an earlier-nonce
// call of the same EVM sender is queued.
type admissionQueue struct {
	// eligible holds the eligible calls best-first, so Min is the best and Max the worst. Every
	// queued call has a unique seq, so no two compare equal and an insert never replaces one.
	// A call's rank does not change while it is in the tree.
	eligible *btree.BTreeG[*insertTicket]
	// senders holds the queued calls of each EVM sender in nonceOrder; only the first is eligible.
	senders map[common.Address][]*insertTicket
	len     uint64
	nextSeq uint64
}

func newAdmissionQueue() *admissionQueue {
	return &admissionQueue{
		eligible: btree.NewG(eligibleDegree, func(a, b *insertTicket) bool { return a.rank.outranks(b.rank) }),
		senders:  map[common.Address][]*insertTicket{},
	}
}

// Len returns the number of queued calls, eligible or not.
func (q *admissionQueue) Len() uint64 { return q.len }

// Best returns the eligible call to admit next.
func (q *admissionQueue) Best() utils.Option[*insertTicket] {
	return someIf(q.eligible.Min())
}

// Worst returns the lowest-ranked eligible call.
func (q *admissionQueue) Worst() utils.Option[*insertTicket] {
	return someIf(q.eligible.Max())
}

// someIf returns Some(t) when ok and None otherwise.
func someIf(t *insertTicket, ok bool) utils.Option[*insertTicket] {
	if !ok {
		return utils.None[*insertTicket]()
	}
	return utils.Some(t)
}

// All yields every queued call.
func (q *admissionQueue) All() iter.Seq[*insertTicket] {
	return func(yield func(*insertTicket) bool) {
		stopped := false
		q.eligible.Ascend(func(t *insertTicket) bool {
			stopped = !t.sender.IsPresent() && !yield(t)
			return !stopped
		})
		if stopped {
			return
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
	if _, found := q.eligible.ReplaceOrInsert(t); found {
		panic("admission queue: two queued calls compare equal")
	}
}

func (q *admissionQueue) makeIneligible(t *insertTicket) {
	if _, found := q.eligible.Delete(t); !found {
		panic("admission queue: an eligible call is missing from the tree")
	}
}
