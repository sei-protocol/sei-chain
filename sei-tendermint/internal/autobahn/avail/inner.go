package avail

import (
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// inner holds roads and lane states.
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
	// setAnchor retains lanes from both anchor committees.
	lanes map[types.LaneID]*laneState
}

type laneState struct {
	blocks *queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]
	votes  *queue[types.BlockNumber, *blockVotes]
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
	nextBlockToPersist types.BlockNumber
}

func newLaneState() *laneState {
	return &laneState{
		blocks: newQueue[types.BlockNumber, *types.Signed[*types.LaneProposal]](),
		votes:  newQueue[types.BlockNumber, *blockVotes](),
	}
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
	first := anchor.FirstIndex()
	epoch := anchor.EpochAt(first).OrPanic("anchor first road epoch")
	roads := newQueue[types.RoadIndex, *road]()
	roads.prune(first)
	i := &inner{
		anchor:                anchor,
		nextCommitQCToPersist: first,
		consensusSpec:         utils.NewAtomicSend(types.ConsensusSpec{CommitQC: anchor.CommitQC(), Epoch: epoch, GenesisTimestamp: genesisTimestamp}),
		roads:                 roads,
		lanes:                 map[types.LaneID]*laneState{},
	}
	i.setAnchor(anchor)
	for _, qc := range loaded.commitQCs {
		if qc.Index() < i.roads.first {
			continue
		}
		if want := i.roads.next; qc.Index() != want {
			return nil, fmt.Errorf("non-contiguous persisted commitQCs: expected %d, got %d", want, qc.Index())
		}
		ep, ok := anchor.EpochAt(qc.Index()).Get()
		if !ok {
			return nil, fmt.Errorf("persisted commitQC %d is outside anchor epochs", qc.Index())
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
	for lane, ls := range i.lanes {
		q := ls.blocks
		for _, b := range blocks[lane] {
			if q.Len() >= BlocksPerLane {
				return fmt.Errorf("lane %s: loaded %d blocks exceeds capacity %d", lane, len(blocks[lane]), BlocksPerLane)
			}
			if b.Number < q.next {
				continue
			}
			if b.Number != q.next {
				return fmt.Errorf("lane %s: non-contiguous persisted blocks: expected %d, got %d", lane, q.next, b.Number)
			}
			// We check the parent hash only for the blocks above the anchor, because:
			// * node can cast LaneVote for the block of the lane without checking the parent hash,
			//   in case the previous block was already (executed and) pruned from memory.
			// * current WAL implementation is lazily pruning on disk, so old executed blocks might be loaded on startup.
			if q.Len() > 0 {
				ph := b.Proposal.Msg().Block().Header().ParentHash()
				if q.q[q.next-1].Msg().Block().Header().Hash() != ph {
					return fmt.Errorf("lane %s: parent hash mismatch at block %d", lane, b.Number)
				}
			}
			q.pushBack(b.Proposal)
		}
		ls.nextBlockToPersist = q.next
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
	epoch, ok := i.anchor.EpochAt(next).Get()
	if !ok {
		// Do NOT advance past the next epoch.
		next -= 1
		epoch = i.anchor.EpochAt(next).OrPanic("persisted road epoch")
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
		// Recount retained block votes under the applied epoch.
		for _, ls := range i.lanes {
			vq := ls.votes
			for n := vq.first; n < vq.next; n++ {
				vq.q[n].reweight(epoch)
			}
		}
	}
}

// laneQC returns the LaneQC for (lane, n) under the applied epoch, if one has formed.
func (i *inner) laneQC(lane types.LaneID, n types.BlockNumber) utils.Option[*types.LaneQC] {
	if ls, ok := i.lanes[lane]; ok {
		if entry, ok := ls.votes.q[n]; ok {
			return entry.qc

		}
	}
	return utils.None[*types.LaneQC]()
}

// setAnchor advances to the data anchor and retains lanes from both anchor epochs.
// It refreshes the consensus spec.
func (i *inner) setAnchor(anchor data.Anchor) {
	i.anchor = anchor
	for _, ep := range anchor.Epochs {
		for lane := range ep.Committee().Lanes().All() {
			if _, ok := i.lanes[lane]; !ok {
				i.lanes[lane] = newLaneState()
			}
		}
	}
	for lane := range i.lanes {
		ok := false
		for _, e := range anchor.Epochs {
			ok = ok || e.Committee().HasLane(lane)
		}
		if !ok {
			delete(i.lanes, lane)
		}
	}
	first := anchor.FirstIndex()
	i.roads.prune(first)
	i.nextCommitQCToPersist = max(i.nextCommitQCToPersist, first)
	qc := anchor.CommitQC()
	for lane, ls := range i.lanes {
		lr := types.LaneRangeOpt(qc, lane)
		ls.votes.prune(lr.Next())
		ls.blocks.prune(lr.Next())
		ls.nextBlockToPersist = max(ls.nextBlockToPersist, lr.Next())
	}
	i.advance()
}
