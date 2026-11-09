package avail

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// blockQueue is one lane's candidate suffix [first, next).
//
// Blocks inside the suffix are parent-linked, so an honest LaneQC voter for its
// tip has the ancestors back to first. The block at first may name any parent,
// so a cut may splice onto a different branch. A CommitQC that chose another
// hash does not rewrite the suffix: execution fetches the chosen body separately
// and verifies it against the FullCommitQC. Prune, driven by the AppQC anchor,
// drops only heights below the anchor's Next(); a later block stays until its
// own height is pruned. parentOfFirstLaneBlock is the parent ProduceLocalBlock
// uses when the suffix is empty. It does not constrain admission.
type blockQueue struct {
	queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]
	// localTip is None, or this node's latest pushed proposal, at a height of at least first-1.
	localTip utils.Option[*types.Signed[*types.LaneProposal]]
	// parentOfFirstLaneBlock is the certified tip at first-1. Zero before any prune.
	parentOfFirstLaneBlock types.BlockHeaderHash
}

func newBlockQueue() *blockQueue {
	return &blockQueue{queue: *newQueue[types.BlockNumber, *types.Signed[*types.LaneProposal]]()}
}

func (q *blockQueue) pushBack(p *types.Signed[*types.LaneProposal]) {
	q.queue.pushBack(p)
	q.localTip = utils.Some(p)
	if q.Len() == 1 {
		q.logLocalBlock()
	}
}

// parentHash is the parent ProduceLocalBlock uses for the block at q.next:
// the in-queue predecessor while first < next, else parentOfFirstLaneBlock.
func (q *blockQueue) parentHash() types.BlockHeaderHash {
	if q.first < q.next {
		return q.q[q.next-1].Msg().Block().Header().Hash()
	}
	return q.parentOfFirstLaneBlock
}

// prune drops [first, newFirst) and records parent as parentOfFirstLaneBlock.
// A stale newFirst changes nothing. localTip is kept when newFirst <= next and
// cleared when newFirst > next. A block at or after newFirst stays.
func (q *blockQueue) prune(newFirst types.BlockNumber, parent types.BlockHeaderHash) {
	if newFirst <= q.first {
		return
	}
	if newFirst > q.next {
		q.localTip = utils.None[*types.Signed[*types.LaneProposal]]()
	}
	q.queue.prune(newFirst)
	q.parentOfFirstLaneBlock = parent
	q.logLocalBlock()
}

// logLocalBlock logs when the block at first does not name parent.
func (q *blockQueue) logLocalBlock() {
	if q.first >= q.next {
		return
	}
	h := q.q[q.first].Msg().Block().Header()
	if h.ParentHash() == q.parentOfFirstLaneBlock {
		return
	}
	logger.Info("local block does not extend certified tip",
		"lane", h.Lane(),
		slog.Uint64("block", uint64(h.BlockNumber())),
		"got", h.ParentHash(),
		"want", q.parentOfFirstLaneBlock)
}

// unpersistedLocalTip returns localTip once it has left the active range and
// block persistence has not reached it.
func (q *blockQueue) unpersistedLocalTip(nextToPersist types.BlockNumber) utils.Option[*types.Signed[*types.LaneProposal]] {
	if p, ok := q.localTip.Get(); ok {
		if n := p.Msg().Block().Header().BlockNumber(); n < q.first && nextToPersist <= n {
			return utils.Some(p)
		}
	}
	return utils.None[*types.Signed[*types.LaneProposal]]()
}

// retentionFloor returns the lowest block number the WAL must still hold.
func (q *blockQueue) retentionFloor() types.BlockNumber {
	if p, ok := q.localTip.Get(); ok {
		return min(q.first, p.Msg().Block().Header().BlockNumber())
	}
	return q.first
}

// inner holds roads and per-LaneID block/vote maps.
type inner struct {
	nextCommitQCToPersist types.RoadIndex // first road whose CommitQC is not persisted
	// consensusSpec is the applied (next-CommitQC) epoch paired with a persisted
	// CommitQC tip (None before the first tip). CommitQC never exceeds
	// persistedCommitQC(). advance publishes the epoch and tip after construction,
	// persistence, or pruning. blockVotes are always weighted under this Epoch.
	consensusSpec utils.AtomicSend[types.ConsensusSpec]
	roads         *queue[types.RoadIndex, *road]

	// anchor contains data's retained certified road and its two epochs.
	// Its first committee verifies departing-lane votes when applied is ahead.
	anchor data.Anchor
	// prune keeps lane maps for the union of the anchor's two committees.
	blocks map[types.LaneID]*blockQueue
	votes  map[types.LaneID]*queue[types.BlockNumber, *blockVotes]
	// nextBlockToPersist tracks per-lane how far block persistence has progressed.
	// RecvBatch only yields blocks below this cursor for voting.
	// Always initialized (even when persistence is disabled — the no-op persist
	// goroutine bumps it immediately). Not persisted to disk: on restart it is
	// reconstructed from the blocks already on disk (see newInner).
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
// newInner requires both to be contiguous and returns an error on gaps. That
// requirement is what makes persist.contiguousSuffix safe: it silently drops
// everything before the last hole it finds, so this is the only thing that
// distinguishes a lazily pruned record from genuinely lost data.
type loadedState struct {
	commitQCs []*types.CommitQC
	blocks    map[types.LaneID][]persist.LoadedBlock
}

// newInner restores WAL records under the anchor's epochs.
func newInner(anchor data.Anchor, genesisTimestamp time.Time, loaded *loadedState) (*inner, error) {
	epoch := anchor.Epochs[0]
	first := anchor.FirstIndex()
	if !epoch.RoadRange().Has(first) {
		epoch = anchor.Epochs[1]
	}
	roads := newQueue[types.RoadIndex, *road]()
	roads.prune(first)
	i := &inner{
		anchor:                anchor,
		nextCommitQCToPersist: first,
		consensusSpec:         utils.NewAtomicSend(types.ConsensusSpec{CommitQC: anchor.CommitQC(), Epoch: epoch, GenesisTimestamp: genesisTimestamp}),
		roads:                 roads,
		blocks:                map[types.LaneID]*blockQueue{},
		votes:                 map[types.LaneID]*queue[types.BlockNumber, *blockVotes]{},
		nextBlockToPersist:    map[types.LaneID]types.BlockNumber{},
	}
	i.prune(anchor)
	for _, qc := range loaded.commitQCs {
		if qc.Index() < i.roads.first {
			continue
		}
		if want := i.roads.next; qc.Index() != want {
			return nil, fmt.Errorf("non-contiguous persisted commitQCs: expected %d, got %d", want, qc.Index())
		}
		ep := anchor.Epochs[0]
		if !ep.RoadRange().Has(qc.Index()) {
			ep = anchor.Epochs[1]
		}
		if err := qc.Verify(ep); err != nil {
			return nil, fmt.Errorf("persisted commitQC %d verify: %w", qc.Index(), err)
		}
		i.roads.pushBack(newRoad(qc, ep))
	}
	i.nextCommitQCToPersist = i.roads.next
	i.advance()
	if err := i.restoreBlocks(loaded.blocks); err != nil {
		return nil, err
	}
	if qc, ok := i.persistedCommitQC().Get(); ok {
		meters.CommitRoadIndex.Set(int64(qc.Index())) // nolint: gosec
	}
	return i, nil
}

// restoreBlocks loads WAL proposals into lane queues. The anchor is persisted
// first and blocks are written sequentially per lane, so gaps, parent-hash
// mismatches, and over-capacity indicate corruption or a bug.
func (i *inner) restoreBlocks(blocks map[types.LaneID][]persist.LoadedBlock) error {
	for lane, q := range i.blocks {
		for _, b := range blocks[lane] {
			if q.Len() >= BlocksPerLane {
				return fmt.Errorf("lane %s: loaded %d blocks exceeds capacity %d", lane, len(blocks[lane]), BlocksPerLane)
			}
			if b.Number < q.next {
				// Certified. Restore localTip from the proposal at first-1 when present.
				if b.Number+1 == q.first {
					q.localTip = utils.Some(b.Proposal)
				}
				continue
			}
			if b.Number != q.next {
				return fmt.Errorf("lane %s: non-contiguous persisted blocks: expected %d, got %d", lane, q.next, b.Number)
			}
			// Parent is checked only inside [first, next). The persisted block at
			// first is this lane's local block. localTip restored from first-1 is
			// for local production, not this check.
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

// persistedCommitQC returns the durable tip from the retained roads or anchor.
func (i *inner) persistedCommitQC() utils.Option[*types.CommitQC] {
	if i.nextCommitQCToPersist > i.roads.first {
		return utils.Some(i.roads.q[i.nextCommitQCToPersist-1].commitQC)
	}
	return i.anchor.CommitQC()
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
	ae := i.anchor.Epochs[0]
	if ae.EpochIndex() == applied.EpochIndex() || !belongs(ae) {
		return utils.None[*types.Epoch]()
	}
	return utils.Some(ae)
}

// advance publishes the consensus spec supported by persisted QCs and anchor epochs.
func (i *inner) advance() {
	// Compute the index of new consensus spec.
	next := i.nextCommitQCToPersist
	epoch := i.anchor.Epochs[0]
	if !epoch.RoadRange().Has(next) {
		epoch = i.anchor.Epochs[1]
		if !epoch.RoadRange().Has(next) {
			next -= 1
		}
	}
	spec := i.consensusSpec.Load()
	if spec.Index() >= next {
		return
	}
	newEpoch := spec.Epoch.EpochIndex() < epoch.EpochIndex()
	spec.Epoch = epoch
	if i.roads.first < next {
		spec.CommitQC = utils.Some(i.roads.q[next-1].commitQC)
	} else {
		spec.CommitQC = i.anchor.CommitQC()
	}
	i.consensusSpec.Store(spec)
	if newEpoch {
		i.reweightVotes()
	}
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

// prune advances to the data anchor and retains lanes from both anchor epochs.
// It refreshes the consensus spec and returns the number of lanes dropped.
func (i *inner) prune(anchor data.Anchor) int {
	i.anchor = anchor
	for _, ep := range anchor.Epochs {
		for lane := range ep.Committee().Lanes().All() {
			i.addLane(lane)
		}
	}
	var obsolete []types.LaneID
	for lane := range i.blocks {
		if !anchor.Epochs[0].Committee().HasLane(lane) && !anchor.Epochs[1].Committee().HasLane(lane) {
			obsolete = append(obsolete, lane)
		}
	}
	dropped := i.dropLanes(obsolete)
	first := anchor.FirstIndex()
	qc := anchor.CommitQC()
	i.roads.prune(first)
	for lane, vq := range i.votes {
		lr := types.LaneRangeOpt(qc, lane)
		bq := i.blocks[lane]
		vq.prune(lr.Next())
		bq.prune(lr.Next(), lr.LastHash())
		// A lagging cursor stops at retentionFloor so an unflushed localTip can still
		// be written. The cursor is never rewound: already past localTip means it is on disk.
		if floor := bq.retentionFloor(); i.nextBlockToPersist[lane] < floor {
			i.nextBlockToPersist[lane] = floor
		}
	}
	i.nextCommitQCToPersist = max(i.nextCommitQCToPersist, first)
	i.advance()
	return dropped
}
