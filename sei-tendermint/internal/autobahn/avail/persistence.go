package avail

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// persisters holds all disk persistence components. Either all are present
// (real I/O) or all are no-op (testing). It is a pure I/O struct — all inner
// state access goes through State methods.
type persisters struct {
	pruneAnchor persist.Persister[*pb.PersistedAvailPruneAnchor]
	blocks      *persist.BlockPersister
	commitQCs   *persist.CommitQCPersister
}

// innerFile is the A/B file prefix for avail inner state persistence.
const innerFile = "avail_inner"

// PruneAnchor is the decoded form of the persisted prune anchor
// (AppQC + matching CommitQC pair). It serves as the crash-recovery
// pruning boundary.
type PruneAnchor struct {
	AppQC    *types.AppQC
	CommitQC *types.CommitQC
}

// PruneAnchorConv converts between PruneAnchor and its protobuf representation.
var PruneAnchorConv = protoutils.Conv[*PruneAnchor, *pb.PersistedAvailPruneAnchor]{
	Encode: func(a *PruneAnchor) *pb.PersistedAvailPruneAnchor {
		return &pb.PersistedAvailPruneAnchor{
			AppQc:    types.AppQCConv.Encode(a.AppQC),
			CommitQc: types.CommitQCConv.Encode(a.CommitQC),
		}
	},
	Decode: func(p *pb.PersistedAvailPruneAnchor) (*PruneAnchor, error) {
		if p.AppQc == nil || p.CommitQc == nil {
			return nil, fmt.Errorf("incomplete prune anchor: AppQC=%v CommitQC=%v", p.AppQc != nil, p.CommitQc != nil)
		}
		appQC, err := types.AppQCConv.Decode(p.AppQc)
		if err != nil {
			return nil, fmt.Errorf("decode AppQC: %w", err)
		}
		commitQC, err := types.CommitQCConv.Decode(p.CommitQc)
		if err != nil {
			return nil, fmt.Errorf("decode CommitQC: %w", err)
		}
		return &PruneAnchor{AppQC: appQC, CommitQC: commitQC}, nil
	},
}

// loadPersistedState creates persisters for the given directory option and loads
// any existing state from disk. When dir is None, all persisters are no-op
// and no state is loaded. When a prune anchor is present, stale commitQCs and
// blocks below the anchor are filtered out before returning.
func loadPersistedState(dir utils.Option[string]) (utils.Option[*loadedAvailState], persisters, error) {
	prunePersister, persistedPruneAnchor, err := persist.NewPersister[*pb.PersistedAvailPruneAnchor](dir, innerFile)
	if err != nil {
		return utils.None[*loadedAvailState](), persisters{}, fmt.Errorf("NewPersister %s: %w", innerFile, err)
	}

	bp, blocks, err := persist.NewBlockPersister(dir)
	if err != nil {
		return utils.None[*loadedAvailState](), persisters{}, fmt.Errorf("NewBlockPersister: %w", err)
	}

	cp, commitQCs, err := persist.NewCommitQCPersister(dir)
	if err != nil {
		return utils.None[*loadedAvailState](), persisters{}, fmt.Errorf("NewCommitQCPersister: %w", err)
	}

	pers := persisters{pruneAnchor: prunePersister, blocks: bp, commitQCs: cp}

	if _, ok := dir.Get(); !ok {
		return utils.None[*loadedAvailState](), pers, nil
	}

	loaded := &loadedAvailState{commitQCs: commitQCs, blocks: blocks}

	if raw, ok := persistedPruneAnchor.Get(); ok {
		anchor, err := PruneAnchorConv.Decode(raw)
		if err != nil {
			return utils.None[*loadedAvailState](), persisters{}, fmt.Errorf("decode prune anchor: %w", err)
		}
		loaded.pruneAnchor = utils.Some(anchor)

		anchorIdx := anchor.AppQC.Proposal().RoadIndex()
		filtered := commitQCs[:0]
		for _, lqc := range commitQCs {
			if lqc.Index >= anchorIdx {
				filtered = append(filtered, lqc)
			}
		}
		loaded.commitQCs = filtered

		for lane, bs := range blocks {
			first := anchor.CommitQC.LaneRange(lane).First()
			j := 0
			for j < len(bs) && bs[j].Number < first {
				j++
			}
			if j > 0 {
				loaded.blocks[lane] = bs[j:]
			}
		}
	}

	return utils.Some(loaded), pers, nil
}

// runPersist is the main loop for the persist goroutine.
// Write order:
//  1. Prune anchor (AppQC + CommitQC pair) — the crash-recovery boundary (sequential).
//  2. commitQCs.MaybePruneAndPersist and each lane's blocks.MaybePruneAndPersistLane run
//     concurrently via scope.Parallel (separate WALs, no early cancellation; first error
//     is returned after all tasks finish).
//     Each path publishes (markCommitQCsPersisted / markBlockPersisted) per entry so voting
//     unblocks ASAP.
//
// On restart, the persisted prune anchor establishes the retained boundary.
//
// TODO: use a single WAL for anchor and CommitQCs to make
// this atomic rather than relying on write order.
func (s *State) runPersist(ctx context.Context, pers persisters) error {
	var persistedAnchorNext types.RoadIndex
	for {
		batch, err := s.collectPersistBatch(ctx, persistedAnchorNext)
		if err != nil {
			return err
		}

		// The same anchor CommitQC drives commit-QC WAL and per-lane block WAL
		// (truncate-then-append below this QC).
		var anchorQC utils.Option[*types.CommitQC]
		// 1. Persist prune anchor first — establishes the crash-recovery boundary.
		if anchor, ok := batch.pruneAnchor.Get(); ok {
			if err := pers.pruneAnchor.Persist(PruneAnchorConv.Encode(anchor)); err != nil {
				return fmt.Errorf("persist prune anchor: %w", err)
			}
			s.advancePersistedBlockStart(anchor.CommitQC)
			persistedAnchorNext = anchor.CommitQC.Proposal().Index() + 1
			anchorQC = utils.Some(anchor.CommitQC)
		}

		markBlock := func(p *types.Signed[*types.LaneProposal]) {
			header := p.Msg().Block().Header()
			s.markBlockPersisted(header.Lane(), header.BlockNumber()+1)
		}

		blocksByLane := make(map[types.LaneID][]*types.Signed[*types.LaneProposal])
		for _, proposal := range batch.blocks {
			lane := proposal.Msg().Block().Header().Lane()
			blocksByLane[lane] = append(blocksByLane[lane], proposal)
		}

		// 2. Persist commit-QCs and per-lane blocks in parallel.
		// Callees handle empty inputs gracefully (no-op when nothing to write/truncate).
		if err := scope.Parallel(func(ps scope.ParallelScope) error {
			ps.Spawn(func() error {
				return pers.commitQCs.MaybePruneAndPersist(anchorQC, batch.commitQCs, utils.Some(func(qc *types.CommitQC) {
					s.markCommitQCsPersisted(qc)
				}))
			})
			// Collect lanes: any lane with blocks in this batch, plus all lanes
			// in the anchor epoch (for WAL pruning).
			// TODO: when epoch transitions land, also union in lanes from all
			// epochs that appear in batch.commitQCs so new-epoch lanes are
			// never skipped in a cross-epoch batch.
			batchLanes := map[types.LaneID]struct{}{}
			for lane := range blocksByLane {
				batchLanes[lane] = struct{}{}
			}
			for _, lane := range batch.pruneLanes {
				batchLanes[lane] = struct{}{}
			}
			for lane := range batchLanes {
				proposals := blocksByLane[lane]
				ps.Spawn(func() error {
					return pers.blocks.MaybePruneAndPersistLane(lane, anchorQC, proposals, utils.Some(markBlock))
				})
			}
			return nil
		}); err != nil {
			return err
		}
	}
}

// persistBatch holds the data collected under lock for one persist iteration.
type persistBatch struct {
	blocks      []*types.Signed[*types.LaneProposal]
	commitQCs   []*types.CommitQC
	pruneAnchor utils.Option[*PruneAnchor]
	pruneLanes  []types.LaneID
}

// advancePersistedBlockStart updates the per-lane block admission boundary
// after durably writing the prune anchor. This unblocks PushBlock/ProduceBlock
// waiters that are gated on persistedBlockFirst + BlocksPerLane.
func (s *State) advancePersistedBlockStart(commitQC *types.CommitQC) {
	for inner, ctrl := range s.inner.Lock() {
		for lane, ls := range inner.lanes.byID {
			ls.durable.advanceFirst(commitQC.LaneRange(lane).First())
		}
		ctrl.Updated()
	}
}

// markBlockPersisted advances the per-lane block persistence cursor.
// Called after each block is persisted so that RecvBatch (and therefore
// voting) can unblock as soon as the block is durable. Safe for concurrent
// callers (acquires s.inner lock internally).
func (s *State) markBlockPersisted(lane types.LaneID, next types.BlockNumber) {
	for inner, ctrl := range s.inner.Lock() {
		ls, ok := inner.lanes.get(lane)
		if !ok {
			return
		}
		ls.durable.advanceNext(next)
		ctrl.Updated()
	}
}

// markCommitQCsPersisted publishes the latest persisted CommitQC,
// gating consensus from advancing until the QC is durable.
func (s *State) markCommitQCsPersisted(qc *types.CommitQC) {
	for inner, ctrl := range s.inner.Lock() {
		inner.commits.markPersisted(qc)
		ctrl.Updated()
	}
}

// collectPersistBatch waits for new blocks or commitQCs and collects them under lock.
func (s *State) collectPersistBatch(ctx context.Context, persistedAnchorNext types.RoadIndex) (persistBatch, error) {
	var b persistBatch
	for inner, ctrl := range s.inner.Lock() {
		// Derive the CommitQC persist cursor from persistedCommitQC. This is
		// safe because persistedCommitQC is only advanced by markCommitQCsPersisted
		// (after disk write) and on startup (from disk). Applying a prune anchor
		// does not update persistedCommitQC, so this always reflects persistence
		// state. The max clamp with commits.qcs.first handles an anchor
		// fast-forwarding the queue past the cursor.
		commitQCNext := types.NextIndexOpt(inner.commits.persistedCommitQC.Load())
		if err := ctrl.WaitUntil(ctx, func() bool {
			if types.NextOpt(inner.app.latestAppQC) != persistedAnchorNext {
				return true
			}
			for _, ls := range inner.lanes.byID {
				if ls.durable.persistedBlockNext < ls.blocks.next {
					return true
				}
			}
			return commitQCNext < inner.commits.qcs.next
		}); err != nil {
			return b, err
		}
		for _, ls := range inner.lanes.byID {
			start := max(ls.durable.persistedBlockNext, ls.blocks.first)
			for n := start; n < ls.blocks.next; n++ {
				b.blocks = append(b.blocks, ls.blocks.q[n])
			}
		}
		commitQCNext = max(commitQCNext, inner.commits.qcs.first)
		for n := commitQCNext; n < inner.commits.qcs.next; n++ {
			b.commitQCs = append(b.commitQCs, inner.commits.qcs.q[n])
		}
		if types.NextOpt(inner.app.latestAppQC) != persistedAnchorNext {
			if appQC, ok := inner.app.latestAppQC.Get(); ok {
				idx := appQC.Proposal().RoadIndex()
				if qc, ok := inner.commits.qcs.q[idx]; ok {
					b.pruneAnchor = utils.Some(&PruneAnchor{
						AppQC:    appQC,
						CommitQC: qc,
					})
					// Capture under the same lock as the anchor so an epoch slide
					// cannot move its committee out of the live duo before I/O.
					ep, err := inner.epoch.duo.Load().EpochForRoad(qc.Proposal().Index())
					if err != nil {
						return b, fmt.Errorf("EpochForRoad(%d): %w", qc.Proposal().Index(), err)
					}
					for lane := range ep.Committee().Lanes().All() {
						b.pruneLanes = append(b.pruneLanes, lane)
					}
				}
			}
		}
	}
	return b, nil
}
