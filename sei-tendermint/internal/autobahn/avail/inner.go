package avail

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// blockQueue is a per-lane block queue.
type blockQueue struct {
	queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]
	// last is None, or this node's last pushed proposal at height >= first-1.
	last utils.Option[*types.Signed[*types.LaneProposal]]
}

func newBlockQueue() *blockQueue {
	return &blockQueue{queue: *newQueue[types.BlockNumber, *types.Signed[*types.LaneProposal]]()}
}

func (q *blockQueue) pushBack(p *types.Signed[*types.LaneProposal]) {
	q.queue.pushBack(p)
	q.last = utils.Some(p)
}

// prune drops [first, newFirst). last is kept when newFirst <= next and
// cleared when newFirst > next.
func (q *blockQueue) prune(newFirst types.BlockNumber) {
	if newFirst <= q.first {
		return
	}
	if newFirst > q.next {
		// TODO: seed last from a non-empty LaneRange LastHash at Next()-1.
		// Empty ranges carry a zero LastHash, so they cannot replace a local last.
		q.last = utils.None[*types.Signed[*types.LaneProposal]]()
	}
	q.queue.prune(newFirst)
}

// unpersistedLast returns the last block once it has left the active range and
// block persistence has not reached it.
func (q *blockQueue) unpersistedLast(nextToPersist types.BlockNumber) utils.Option[*types.Signed[*types.LaneProposal]] {
	if p, ok := q.last.Get(); ok {
		if n := p.Msg().Block().Header().BlockNumber(); n < q.first && nextToPersist <= n {
			return utils.Some(p)
		}
	}
	return utils.None[*types.Signed[*types.LaneProposal]]()
}

// retentionFloor returns the lowest block number the WAL must still hold.
func (q *blockQueue) retentionFloor() types.BlockNumber {
	if p, ok := q.last.Get(); ok {
		return min(q.first, p.Msg().Block().Header().BlockNumber())
	}
	return q.first
}

// inner holds roads and per-LaneID block/vote maps.
type inner struct {
	persistedCommitQC utils.AtomicSend[utils.Option[*types.CommitQC]] // latest persisted CommitQC
	// consensusSpec is the applied (next-CommitQC) epoch paired with a persisted
	// CommitQC tip (None before the first tip). CommitQC never exceeds
	// persistedCommitQC. advanceEpoch installs Epoch; construction and
	// runEpochAdvance are the writers. blockVotes are always weighted under this Epoch.
	consensusSpec utils.AtomicSend[types.ConsensusSpec]
	roads         *queue[types.RoadIndex, *road]

	// anchorEpoch is the epoch of data's Anchor CommitQC when one exists.
	// None until the first Anchor arrives (construction prune or runEvict).
	// When it lags applied, epochForVote falls back to this committee for
	// departing-lane voters.
	anchorEpoch utils.Option[*types.Epoch]
	blocks      map[types.LaneID]*blockQueue
	votes       map[types.LaneID]*queue[types.BlockNumber, *blockVotes]
	// nextBlockToPersist tracks per-lane how far block persistence has progressed.
	// RecvBatch only yields blocks below this cursor for voting.
	// Always initialized (even when persistence is disabled — the no-op persist
	// goroutine bumps it immediately). Not persisted to disk: on restart it is
	// reconstructed from the blocks already on disk (see restoreInner).
	//
	// TODO: consider giving this its own AtomicSend to avoid waking unrelated
	// inner waiters (PushVote, PushCommitQC, etc.) on setNextBlockToPersist calls.
	// Now that blocks are persisted concurrently by lane (one notification per
	// lane per batch, not per block), the frequency is lower, but still not
	// ideal. Only RecvBatch needs to be notified of cursor changes;
	// collectPersistBatch is in the same goroutine and reads it directly.
	nextBlockToPersist map[types.LaneID]types.BlockNumber
}

// loadedState holds data loaded from disk on restart.
// commitQCs are sorted by road index; blocks are sorted by number per lane.
// restoreInner requires both to be contiguous and returns an error on gaps. That
// requirement is what makes persist.contiguousSuffix safe: it silently drops
// everything before the last hole it finds, so this is the only thing that
// distinguishes a lazily pruned record from genuinely lost data.
type loadedState struct {
	commitQCs []*types.CommitQC
	blocks    map[types.LaneID][]persist.LoadedBlock
}

func newInner(ep *types.Epoch, first types.RoadIndex) *inner {
	roads := newQueue[types.RoadIndex, *road]()
	roads.first = first
	roads.next = first
	i := &inner{
		persistedCommitQC:  utils.NewAtomicSend(utils.None[*types.CommitQC]()),
		consensusSpec:      utils.NewAtomicSend(types.ConsensusSpec{CommitQC: utils.None[*types.CommitQC](), Epoch: ep}),
		roads:              roads,
		blocks:             map[types.LaneID]*blockQueue{},
		votes:              map[types.LaneID]*queue[types.BlockNumber, *blockVotes]{},
		nextBlockToPersist: map[types.LaneID]types.BlockNumber{},
	}
	for lane := range ep.Committee().Lanes().All() {
		i.addLane(lane)
	}
	return i
}

// restoreBlocks loads WAL proposals into lane queues. The anchor is persisted
// first and blocks are written sequentially per lane, so gaps, parent-hash
// mismatches, and over-capacity indicate corruption or a bug.
func (i *inner) restoreBlocks(blocks map[types.LaneID][]persist.LoadedBlock) error {
	for lane, bs := range blocks {
		q, ok := i.blocks[lane]
		if !ok || len(bs) == 0 {
			continue
		}
		for _, b := range bs {
			if q.Len() >= BlocksPerLane {
				return fmt.Errorf("lane %s: loaded %d blocks exceeds capacity %d", lane, len(bs), BlocksPerLane)
			}
			if b.Number < q.next {
				// Certified. Restore last from the proposal at first-1 when present.
				if b.Number+1 == q.first {
					q.last = utils.Some(b.Proposal)
				}
				continue
			}
			if b.Number != q.next {
				return fmt.Errorf("lane %s: non-contiguous persisted blocks: expected %d, got %d", lane, q.next, b.Number)
			}
			// Parent is checked only inside [first, next). last restored from
			// first-1 is for local production, not this check.
			if q.first < q.next {
				ph := b.Proposal.Msg().Block().Header().ParentHash()
				if q.q[q.next-1].Msg().Block().Header().Hash() != ph {
					return fmt.Errorf("lane %s: parent hash mismatch at block %d", lane, b.Number)
				}
			}
			q.pushBack(b.Proposal)
		}
		i.nextBlockToPersist[lane] = q.next
	}
	return nil
}

func (i *inner) applied() *types.Epoch {
	return i.consensusSpec.Load().Epoch
}

// epochForVote returns the applied or Anchor epoch the vote belongs to
// (lane + signer in that committee). Prefers applied; falls back to Anchor
// when that is a different EpochIndex.
func (i *inner) epochForVote(vote *types.Signed[*types.LaneVote]) utils.Option[*types.Epoch] {
	lane := vote.Msg().Header().Lane()
	key := vote.Key()
	belongs := func(ep *types.Epoch) bool {
		c := ep.Committee()
		return c.HasLane(lane) && c.HasReplica(key)
	}
	applied := i.applied()
	if belongs(applied) {
		return utils.Some(applied)
	}
	ae, ok := i.anchorEpoch.Get()
	if !ok || ae.EpochIndex() == applied.EpochIndex() || !belongs(ae) {
		return utils.None[*types.Epoch]()
	}
	return utils.Some(ae)
}

// advanceEpoch makes ep the applied epoch: opens its lanes, reweights votes,
// and publishes ConsensusSpec for the durable tip.
func (i *inner) advanceEpoch(ep *types.Epoch) {
	for lane := range ep.Committee().Lanes().All() {
		i.addLane(lane)
	}
	i.consensusSpec.Store(types.ConsensusSpec{CommitQC: i.persistedCommitQC.Load(), Epoch: ep})
	i.reweightVotes()
}

// advanceTarget is the epoch to install next: the epoch of the road following
// the durable tip, or applied+1 while that road is still inside the applied
// epoch.
func (i *inner) advanceTarget() types.EpochIndex {
	next := i.applied().EpochIndex() + 1
	tip, ok := i.persistedCommitQC.Load().Get()
	if !ok {
		return next
	}
	return max(next, epoch.IndexForRoad(tip.Index()+1))
}

// canAdvanceEpoch reports whether the applied epoch is sealed and its prune leash is
// met for a one-step advance to applied+1. Sealed means roads and persistedCommitQC
// hold the epoch's last CommitQC. The prune leash is met when the Anchor epoch
// covers the applied epoch. The execution leash is checked separately.
func (i *inner) canAdvanceEpoch() bool {
	ep := i.applied()
	if i.roads.next < ep.RoadRange().Next {
		return false
	}
	tip, ok := i.persistedCommitQC.Load().Get()
	if !ok || tip.Index()+1 < ep.RoadRange().Next {
		return false
	}
	ae, ok := i.anchorEpoch.Get()
	return ok && ae.EpochIndex() >= ep.EpochIndex()
}

// refreshConsensusSpec publishes the durable tip when the following RoadIndex
// sits in the applied epoch. Otherwise the previous spec stands (withhold at
// LastRoad until advanceEpoch). It does not change Epoch; advanceEpoch does.
func (i *inner) refreshConsensusSpec() {
	tip := i.persistedCommitQC.Load()
	cqc, ok := tip.Get()
	if !ok {
		return
	}
	next := cqc.Index() + 1
	ep := i.applied()
	if !ep.RoadRange().Has(next) {
		return
	}
	i.consensusSpec.Store(types.ConsensusSpec{CommitQC: tip, Epoch: ep})
}

func (i *inner) addLane(lane types.LaneID) bool {
	if _, ok := i.blocks[lane]; ok {
		return false
	}
	i.blocks[lane] = newBlockQueue()
	i.votes[lane] = newQueue[types.BlockNumber, *blockVotes]()
	i.nextBlockToPersist[lane] = 0
	return true
}

func (i *inner) dropLanes(lanes []types.LaneID) int {
	n := 0
	for _, lane := range lanes {
		if _, ok := i.blocks[lane]; !ok {
			continue
		}
		delete(i.blocks, lane)
		delete(i.votes, lane)
		delete(i.nextBlockToPersist, lane)
		n++
	}
	return n
}

// laneQC returns the LaneQC for (lane, n) under the applied epoch, if one has formed.
func (i *inner) laneQC(lane types.LaneID, n types.BlockNumber) utils.Option[*types.LaneQC] {
	votes, ok := i.votes[lane]
	if !ok {
		return utils.None[*types.LaneQC]()
	}
	entry, ok := votes.q[n]
	if !ok {
		return utils.None[*types.LaneQC]()
	}
	return entry.qc
}

// reweightVotes recounts retained block votes under the applied epoch.
func (i *inner) reweightVotes() {
	ep := i.applied()
	for _, vq := range i.votes {
		for n := vq.first; n < vq.next; n++ {
			vq.q[n].reweight(ep)
		}
	}
}

// prune advances the state up to the data Anchor and drops lanes closed as of
// anchor.Epoch. It updates anchorEpoch and refreshes ConsensusSpec's tip.
// Applied epoch changes belong to construction and runEpochAdvance.
// Returns the number of lanes dropped.
func (i *inner) prune(anchor data.Anchor) int {
	anchorEpoch := anchor.Epoch
	idx := anchor.CommitQC.Index()
	i.roads.prune(idx + 1)
	for lane, vq := range i.votes {
		lr := anchor.CommitQC.LaneRange(lane)
		bq := i.blocks[lane]
		vq.prune(lr.Next())
		bq.prune(lr.Next())
		// A lagging cursor stops at retentionFloor so an unflushed last can still
		// be written. The cursor is never rewound: already past last means it is on disk.
		if floor := bq.retentionFloor(); i.nextBlockToPersist[lane] < floor {
			i.nextBlockToPersist[lane] = floor
		}
	}
	if i.roads.Len() == 0 {
		i.persistedCommitQC.Store(utils.Some(anchor.CommitQC))
	}
	i.anchorEpoch = utils.Some(anchorEpoch)
	var closed []types.LaneID
	for lane := range i.blocks {
		if anchorEpoch.IsClosed(lane) {
			closed = append(closed, lane)
		}
	}
	n := i.dropLanes(closed)
	i.refreshConsensusSpec()
	return n
}
