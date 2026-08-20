package avail

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// inner holds roads and per-LaneID block/vote maps.
type inner struct {
	persistedCommitQC utils.AtomicSend[utils.Option[*types.CommitQC]] // latest persisted CommitQC
	// consensusSpec is the applied (next-CommitQC) epoch paired with a persisted
	// CommitQC tip (None before the first tip). CommitQC never exceeds
	// persistedCommitQC. advanceEpoch is the sole writer of Epoch after
	// construction. blockVotes are always weighted under this Epoch.
	consensusSpec utils.AtomicSend[types.ConsensusSpec]
	roads         *queue[types.RoadIndex, *road]

	// anchorEpoch is the epoch of data's Anchor CommitQC when one exists.
	// None until the first Anchor arrives (construction prune or runEvict).
	// When it lags applied, epochForVote falls back to this committee for
	// departing-lane voters. prune never advances applied — only advanceEpoch does.
	anchorEpoch utils.Option[*types.Epoch]
	blocks      map[types.LaneID]*queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]
	votes       map[types.LaneID]*queue[types.BlockNumber, *blockVotes]
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

func newInner(ds *data.State, loaded *loadedState) (*inner, error) {
	genesis, err := ds.Registry().EpochByIndex(0)
	if err != nil {
		return nil, fmt.Errorf("genesis epoch 0: %w", err)
	}
	i := &inner{
		persistedCommitQC:  utils.NewAtomicSend(utils.None[*types.CommitQC]()),
		consensusSpec:      utils.NewAtomicSend(types.ConsensusSpec{CommitQC: utils.None[*types.CommitQC](), Epoch: genesis}),
		roads:              newQueue[types.RoadIndex, *road](),
		blocks:             map[types.LaneID]*queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]{},
		votes:              map[types.LaneID]*queue[types.BlockNumber, *blockVotes]{},
		nextBlockToPersist: map[types.LaneID]types.BlockNumber{},
	}
	for lane := range genesis.Committee().Lanes().All() {
		i.addLane(lane)
	}
	for lane := range loaded.blocks {
		i.addLane(lane)
	}

	// Apply the persisted prune anchor from the data.State.
	if anchor, ok := ds.Anchor().Load().Get(); ok {
		i.prune(anchor)
	}

	// Restore persisted CommitQCs. prune() may have already pushed the
	// anchor's CommitQC, so skip entries below commitQCs.next.
	for _, qc := range loaded.commitQCs {
		if qc.Index() < i.roads.next {
			continue
		}
		if qc.Index() != i.roads.next {
			return nil, fmt.Errorf("non-contiguous persisted commitQCs: expected %d, got %d", i.roads.next, qc.Index())
		}
		ep, err := ds.Registry().EpochByIndex(qc.Proposal().EpochIndex())
		if err != nil {
			return nil, fmt.Errorf("persisted commitQC %d epoch: %w", qc.Index(), err)
		}
		if err := qc.Verify(ep); err != nil {
			return nil, fmt.Errorf("persisted commitQC %d verify: %w", qc.Index(), err)
		}
		i.roads.pushBack(newRoad(qc, ep))
	}
	if i.roads.Len() > 0 {
		last := i.roads.q[i.roads.next-1]
		i.persistedCommitQC.Store(utils.Some(last.commitQC))
		i.seedApplied(last.epoch)
	} else if ae, ok := i.anchorEpoch.Get(); ok {
		i.seedApplied(ae)
	}

	// Restore persisted blocks. Since the anchor is persisted first and
	// blocks are written sequentially per lane, gaps, parent-hash
	// mismatches, and over-capacity indicate corruption or a bug.
	for lane, bs := range loaded.blocks {
		q, ok := i.blocks[lane]
		if !ok || len(bs) == 0 {
			continue
		}
		for _, b := range bs {
			if q.Len() >= BlocksPerLane {
				return nil, fmt.Errorf("lane %s: loaded %d blocks exceeds capacity %d", lane, len(bs), BlocksPerLane)
			}
			if b.Number < q.next {
				continue
			}
			if b.Number != q.next {
				return nil, fmt.Errorf("lane %s: non-contiguous persisted blocks: expected %d, got %d", lane, q.next, b.Number)
			}
			// We check the parent hash only for the blocks above the anchor, because:
			// * node can cast LaneVote for the block of the lane without checking the parent hash,
			//   in case the previous block was already (executed and) pruned from memory.
			// * current WAL implementation is lazily pruning on disk, so old executed blocks might be loaded on startup.
			if q.Len() > 0 {
				ph := b.Proposal.Msg().Block().Header().ParentHash()
				if q.q[q.next-1].Msg().Block().Header().Hash() != ph {
					return nil, fmt.Errorf("lane %s: parent hash mismatch at block %d", lane, b.Number)
				}
			}
			q.pushBack(b.Proposal)
		}
		i.nextBlockToPersist[lane] = q.next
	}
	// seedApplied jumped to the last restored QC's epoch (or the Anchor).
	// One more step covers a LastRoad tip whose next epoch is already registered.
	if err := i.tryAdvanceEpoch(ds); err != nil {
		return nil, err
	}
	i.refreshConsensusSpec()
	return i, nil
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

// seedApplied sets applied to ep and opens its lanes. Construction only;
// live advances go through advanceEpoch.
func (i *inner) seedApplied(ep *types.Epoch) {
	for lane := range ep.Committee().Lanes().All() {
		i.addLane(lane)
	}
	spec := i.consensusSpec.Load()
	i.consensusSpec.Store(types.ConsensusSpec{CommitQC: spec.CommitQC, Epoch: ep})
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

// canAdvanceEpoch reports whether the applied epoch is sealed and its prune leash is
// met. Sealed means roads and persistedCommitQC hold the epoch's last CommitQC.
// Waiting on persist keeps applied in lockstep with consensusSpec.Epoch.
// The prune leash is met when the Anchor epoch covers the applied epoch.
// The execution leash — registry contains the next epoch — is checked
// separately so live waiters are not parked on avail's lock for a registry update.
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

// tryAdvanceEpoch advances applied by one epoch when the seal and prune
// leashes are already met. A missing next registry epoch in that state is an
// invariant violation (execution leash should already have registered it).
func (i *inner) tryAdvanceEpoch(ds *data.State) error {
	if !i.canAdvanceEpoch() {
		return nil
	}
	nextIdx := i.applied().EpochIndex() + 1
	next, err := ds.Registry().EpochByIndex(nextIdx)
	if err != nil {
		return fmt.Errorf("epoch %d with seal+prune leashes met: %w", nextIdx, err)
	}
	i.advanceEpoch(next)
	return nil
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
	i.blocks[lane] = newQueue[types.BlockNumber, *types.Signed[*types.LaneProposal]]()
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
// anchor.Epoch. It updates anchorEpoch only — applied is advanced by
// runEpochAdvance. Returns the number of lanes dropped.
func (i *inner) prune(anchor data.Anchor) int {
	anchorEpoch := anchor.Epoch
	idx := anchor.CommitQC.Index()
	if idx >= i.roads.first {
		i.roads.prune(idx + 1)
		for lane, vq := range i.votes {
			lr := anchor.CommitQC.LaneRange(lane)
			bq := i.blocks[lane]
			vq.prune(lr.Next())
			bq.prune(lr.Next())
			if i.nextBlockToPersist[lane] < lr.Next() {
				i.nextBlockToPersist[lane] = lr.Next()
			}
		}
		if i.roads.Len() == 0 {
			i.persistedCommitQC.Store(utils.Some(anchor.CommitQC))
		}
	}
	i.anchorEpoch = utils.Some(anchorEpoch)
	var closed []types.LaneID
	for lane := range i.blocks {
		if anchorEpoch.IsClosed(lane) {
			closed = append(closed, lane)
		}
	}
	return i.dropLanes(closed)
}
