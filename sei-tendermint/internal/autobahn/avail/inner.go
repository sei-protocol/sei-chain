package avail

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// inner holds roads and per-LaneID block/vote maps.
type inner struct {
	persistedCommitQC utils.AtomicSend[utils.Option[*types.CommitQC]] // latest persisted CommitQC
	consensusSpec     utils.AtomicSend[types.ConsensusSpec]
	roads             *queue[types.RoadIndex, *road]

	// epoch is the applied (next-CommitQC) epoch. advanceEpoch is the sole
	// writer after construction. Distinct from consensusSpec.Epoch, which is the
	// epoch of the RoadIndex after the publishable tip and may lag while withheld.
	// blockVotes are always weighted under this epoch.
	epoch utils.AtomicSend[*types.Epoch]
	// anchorEpoch is the epoch of data's Anchor CommitQC when one exists.
	// None until the first Anchor arrives (construction prune or runEvict).
	// It may exceed the applied epoch while runEpochAdvance is parked on
	// WaitForEpoch, or briefly between prune and the next advance: admission
	// falls back to the Anchor committee via epochForVote / epochForLane.
	// prune never advances i.epoch — only advanceEpoch does.
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
		epoch:              utils.NewAtomicSend(genesis),
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
	// Restart catch-up: advance every epoch the durable leashes already allow.
	// The live path (runEpochAdvance) advances one waited-for epoch at a time.
	if err := i.advanceReadyEpochs(ds); err != nil {
		return nil, err
	}
	i.refreshConsensusSpec()
	return i, nil
}

// seedApplied sets applied to ep and opens its lanes. Construction only;
// live advances go through advanceEpoch.
func (i *inner) seedApplied(ep *types.Epoch) {
	for lane := range ep.Committee().Lanes().All() {
		i.addLane(lane)
	}
	i.epoch.Store(ep)
}

// advanceEpoch makes ep the applied epoch: opens its lanes, reweights votes,
// and republishes ConsensusSpec.
func (i *inner) advanceEpoch(ep *types.Epoch) {
	for lane := range ep.Committee().Lanes().All() {
		i.addLane(lane)
	}
	// Publish applied epoch before reweight so reweightVotes reads i.epoch.
	// Callers hold the avail lock, so Epoch() waiters cannot observe votes
	// between the Store and the reweight.
	i.epoch.Store(ep)
	i.reweightVotes()
	i.refreshConsensusSpec()
}

// canAdvanceEpoch reports whether the applied epoch is sealed and its prune leash is
// met. Sealed means roads hold the epoch's last CommitQC. The prune leash is met
// when the Anchor epoch covers the applied epoch (an AppQC for that epoch
// exists). The execution leash — registry contains the next epoch — is checked
// separately so live waiters are not parked on avail's lock for a registry update.
func (i *inner) canAdvanceEpoch() bool {
	ep := i.epoch.Load()
	if i.roads.next < ep.RoadRange().Next {
		return false
	}
	ae, ok := i.anchorEpoch.Get()
	return ok && ae.EpochIndex() >= ep.EpochIndex()
}

// advanceReadyEpochs advances to every epoch whose seal and prune leashes are
// already met. A missing next registry epoch in that state is an invariant
// violation (execution leash should already have registered it).
func (i *inner) advanceReadyEpochs(ds *data.State) error {
	for i.canAdvanceEpoch() {
		nextIdx := i.epoch.Load().EpochIndex() + 1
		next, err := ds.Registry().EpochByIndex(nextIdx)
		if err != nil {
			return fmt.Errorf("epoch %d with seal+prune leashes met: %w", nextIdx, err)
		}
		i.advanceEpoch(next)
	}
	return nil
}

// refreshConsensusSpec publishes ConsensusSpec for the durable tip, paired with
// the epoch of the RoadIndex that follows it. The spec is withheld — the
// previously published one stands — until that epoch is applied and resolvable.
//
// Withholding rather than publishing an earlier tip is what keeps the spec
// monotonic. At an epoch boundary the durable tip sits on LastRoad(E) while
// applied is still E, and a node that already entered E+1 before a restart must
// not be handed a predecessor of the tip it holds: advancing to it would roll the
// tip backwards and discard that view's votes.
func (i *inner) refreshConsensusSpec() {
	tip := i.persistedCommitQC.Load()
	cqc, ok := tip.Get()
	if !ok {
		return
	}
	next := cqc.Index() + 1
	ep := i.epoch.Load()
	if epoch.IndexForRoad(next) > ep.EpochIndex() {
		return
	}
	if !ep.RoadRange().Has(next) {
		// Persist may lag advanceEpoch: tip's next RoadIndex can sit in an
		// earlier epoch still present on some admitted road.
		found := false
		if next >= i.roads.first && next < i.roads.next {
			ep = i.roads.q[next].epoch
			found = true
		} else {
			for idx := i.roads.first; idx < i.roads.next; idx++ {
				if r := i.roads.q[idx].epoch; r.RoadRange().Has(next) {
					ep = r
					found = true
					break
				}
			}
		}
		if !found {
			return
		}
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

// laneQC returns the LaneQC for (lane, n) under the applied epoch's vote
// weighting (i.epoch), if one has formed.
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

// reweightVotes recounts retained block votes under the applied epoch (i.epoch).
func (i *inner) reweightVotes() {
	ep := i.epoch.Load()
	for _, vq := range i.votes {
		for n := vq.first; n < vq.next; n++ {
			vq.q[n].reweight(ep)
		}
	}
}

// prune advances the state up to the data Anchor and drops lanes closed as of
// anchor.Epoch. It updates anchorEpoch only — applied epoch catch-up is left to
// advanceReadyEpochs / runEpochAdvance. Returns the number of lanes dropped.
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
