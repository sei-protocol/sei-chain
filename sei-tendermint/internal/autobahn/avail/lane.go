package avail

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// NextBlock returns the index of the next missing block in local storage for the given lane.
func (s *State) NextBlock(lane types.LaneID) types.BlockNumber {
	for inner := range s.inner.Lock() {
		if ls, ok := inner.lanes.get(lane); ok {
			return ls.blocks.next
		}
	}
	return 0
}

// Block returns block n of the given lane.
// Waits until the block is available.
// Returns ErrPruned if the block has been already pruned.
func (s *State) Block(ctx context.Context, lane types.LaneID, n types.BlockNumber) (*types.Signed[*types.LaneProposal], error) {
	for inner, ctrl := range s.inner.Lock() {
		ls, ok := inner.lanes.get(lane)
		if !ok {
			return nil, ErrBadLane
		}
		q := ls.blocks
		if err := ctrl.WaitUntil(ctx, func() bool { return n < q.next }); err != nil {
			return nil, err
		}
		if n < q.first {
			return nil, types.ErrPruned
		}
		return q.q[n], nil
	}
	panic("unreachable")
}

// PushBlock pushes a block to the state.
// Waits until all previous blocks are available.
func (s *State) PushBlock(ctx context.Context, p *types.Signed[*types.LaneProposal]) error {
	h := p.Msg().Block().Header()
	if p.Key() != h.Lane() {
		return fmt.Errorf("signer %v does not match lane %v", p.Key(), h.Lane())
	}
	// Snapshot Current once for off-lock verify. Unlike PushVote (which parks
	// until Current accepts the signer), we do not wait for future committees —
	// lane proposals are not reweighted across epoch advances.
	duo := s.epochDuo.Load()
	c := duo.Current.Committee()
	if err := p.Msg().Verify(c); err != nil {
		return fmt.Errorf("block.Verify(): %w", err)
	}
	if err := p.VerifySig(c); err != nil {
		return fmt.Errorf("block.VerifySig(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		ls, ok := inner.lanes.get(h.Lane())
		if !ok {
			return ErrBadLane
		}
		q := ls.blocks
		if err := ctrl.WaitUntil(ctx, func() bool {
			return h.BlockNumber() <= min(q.next, ls.durable.admitLimit()-1)
		}); err != nil {
			return err
		}
		// not needed any more
		if q.next != h.BlockNumber() {
			return nil
		}
		// Verify parent hash chain to prevent a malicious producer from
		// breaking the block chain, which would deadlock header reconstruction.
		// A mismatch means the producer equivocated (produced a different
		// chain than we already have). We log it to aid debugging stalled
		// lanes but do not return an error — the caller should not tear
		// down the peer connection over an equivocating producer.
		// NOTE: after pruning (q.first >= q.next), we cannot verify the parent
		// hash because the previous block is gone. This is safe because
		// headers() never follows the first block's parentHash in a LaneRange.
		if q.first < q.next {
			prevHash := q.q[q.next-1].Msg().Block().Header().Hash()
			if h.ParentHash() != prevHash {
				logger.Error("parent hash mismatch (producer equivocation)",
					"lane", h.Lane(),
					slog.Uint64("block", uint64(h.BlockNumber())),
					"got", h.ParentHash(),
					"want", prevHash)
				return nil
			}
		}
		q.pushBack(p)
		ctrl.Updated()
	}
	return nil
}

// PushVote parks until Current can accept the vote (signer weight + voted lane),
// verifies, then under lock waits for capacity and credits with the live duo
// (drop if Current advanced and signer left).
//
// Lane-vote streams are committee-only (giga RunServer/RunClient), so parking a
// future-epoch signer does not expose an unauthenticated DoS path. No p2p retry:
// without this wait, async epoch entry would drop the vote permanently.
func (s *State) PushVote(ctx context.Context, vote *types.Signed[*types.LaneVote]) error {
	h := vote.Msg().Header()
	// Future-epoch voters park (one stream goroutine) until Current includes them.
	var committee *types.Committee
	var verifiedEpoch types.EpochIndex
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			c := inner.epoch.duo.Load().Current.Committee()
			return c.Weight(vote.Key()) > 0 && c.HasLane(h.Lane())
		}); err != nil {
			return err
		}
		duo := inner.epoch.duo.Load()
		committee = duo.Current.Committee()
		verifiedEpoch = duo.Current.EpochIndex()
	}
	if err := vote.Msg().Verify(committee); err != nil {
		return fmt.Errorf("vote.Verify(): %w", err)
	}
	if err := vote.VerifySig(committee); err != nil {
		return fmt.Errorf("vote.Verify(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		ls, ok := inner.lanes.get(h.Lane())
		if !ok {
			return ErrBadLane
		}
		q := ls.votes
		if err := ctrl.WaitUntil(ctx, func() bool {
			return h.BlockNumber() < ls.durable.admitLimit()
		}); err != nil {
			return err
		}
		// WaitUntil may release the lock; re-check membership under live Current.
		live := inner.epoch.duo.Load()
		if live.Current.EpochIndex() != verifiedEpoch &&
			(live.Current.Committee().Weight(vote.Key()) == 0 ||
				!live.Current.Committee().HasLane(h.Lane())) {
			return nil
		}
		if h.BlockNumber() < q.first {
			return nil
		}
		for q.next <= h.BlockNumber() {
			q.pushBack(newBlockVotes())
		}
		if q.q[h.BlockNumber()].pushVote(live.Current, vote).IsPresent() {
			ctrl.Updated()
		}
	}
	return nil
}

// headers collects headers for the given range.
func (s *State) headers(ctx context.Context, lr *types.LaneRange) ([]*types.BlockHeader, error) {
	// Empty range is always available.
	if lr.First() == lr.Next() {
		return nil, nil
	}
	want := lr.LastHash()
	headers := make([]*types.BlockHeader, lr.Next()-lr.First())
	for inner, ctrl := range s.inner.Lock() {
		ls, ok := inner.lanes.get(lr.Lane())
		if !ok {
			return nil, types.ErrPruned
		}
		q := ls.votes
		for i := range headers {
			n := lr.Next() - types.BlockNumber(i) - 1 //nolint:gosec // i is bounded by len(headers) which is a small block range; no overflow risk
			for {
				// If pruned, then give up.
				if q.first > lr.First() {
					return nil, types.ErrPruned
				}
				if bv := q.q[n]; bv != nil {
					if set, ok := bv.byHash[want]; ok {
						want = set.header.ParentHash()
						headers[len(headers)-i-1] = set.header
						break
					}
				}
				// Otherwise, wait.
				if err := ctrl.Wait(ctx); err != nil {
					return nil, err
				}
			}
		}
	}
	return headers, nil
}

// WaitForLocalCapacity waits until the lane owned by this node has capacity for toProduce block.
func (s *State) WaitForLocalCapacity(ctx context.Context, toProduce types.BlockNumber) error {
	lane := s.key.Public()
	for inner, ctrl := range s.inner.Lock() {
		ls, ok := inner.lanes.get(lane)
		if !ok {
			return ErrBadLane
		}
		if err := ctrl.WaitUntil(ctx, func() bool {
			return toProduce < ls.durable.admitLimit()
		}); err != nil {
			return err
		}
	}
	return nil
}

// WaitForLaneQCs waits until there is at least 1 LaneQC in the Current epoch
// with a block not finalized by prev. Returns the Current epoch alongside the
// QCs so the caller can verify it matches the epoch it intends to propose in.
func (s *State) WaitForLaneQCs(
	ctx context.Context, prev utils.Option[*types.CommitQC],
) (map[types.LaneID]*types.LaneQC, *types.Epoch, error) {
	for inner, ctrl := range s.inner.Lock() {
		laneQCs := map[types.LaneID]*types.LaneQC{}
		for {
			ep := inner.epoch.duo.Load().Current
			for lane := range ep.Committee().Lanes().All() {
				first := types.LaneRangeOpt(prev, lane).Next()
				for i := range types.BlockNumber(types.MaxLaneRangeInProposal) {
					if qc, ok := inner.lanes.laneQC(lane, first+i).Get(); ok {
						laneQCs[lane] = qc
					} else {
						break
					}
				}
			}
			if len(laneQCs) > 0 {
				return laneQCs, ep, nil
			}
			if err := ctrl.Wait(ctx); err != nil {
				return nil, nil, err
			}
		}
	}
	panic("unreachable")
}

// ProduceLocalBlock appends a new block to the producers lane.
// Fails in case there is not enough capacity in the lane, or it is not the next block expected.
func (s *State) ProduceLocalBlock(n types.BlockNumber, payload *types.Payload) (*types.Signed[*types.LaneProposal], error) {
	return s.produceLocalBlock(n, s.key, payload)
}

// TODO: produceLocalBlock is a separate function for testing - consider improving the tests to use ProduceBlock only.
func (s *State) produceLocalBlock(n types.BlockNumber, key types.SecretKey, payload *types.Payload) (*types.Signed[*types.LaneProposal], error) {
	lane := key.Public()
	var result *types.Signed[*types.LaneProposal]
	for inner, ctrl := range s.inner.Lock() {
		ls, ok := inner.lanes.get(lane)
		if !ok {
			return nil, ErrBadLane
		}
		q := ls.blocks
		if n >= ls.durable.admitLimit() {
			return nil, fmt.Errorf("lane full")
		}
		if q.next != n {
			return nil, fmt.Errorf("unexpected block number: got %v, want %v", n, q.next)
		}
		var parent types.BlockHeaderHash
		if q.first < q.next {
			parent = q.q[q.next-1].Msg().Block().Header().Hash()
		}
		result = types.Sign(key, types.NewLaneProposal(types.NewBlock(lane, q.next, parent, payload)))
		q.pushBack(result)
		ctrl.Updated()
	}
	return result, nil
}
