package avail

import (
	"context"
	"errors"
	"fmt"

	"github.com/tendermint/tendermint/internal/autobahn/data"
	"github.com/tendermint/tendermint/internal/autobahn/types"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
)

// ErrBadLane .
var ErrBadLane = errors.New("bad lane")

const blocksPerLane = 3 * blocksPerLanePerCommit
const blocksPerLanePerCommit = 10

// State is the block availability state provided by the node for consensus.
// It contains:
// * commitQCs
// * locally available blocks
// * availability votes (LaneVotes) of other validators.
// * execution votes (AppVotes) of other validators.
type State struct {
	key   types.SecretKey
	data  *data.State
	inner utils.Watch[*inner]
}

// NewState constructs a new availability state.
func NewState(key types.SecretKey, data *data.State) *State {
	return &State{
		key:   key,
		data:  data,
		inner: utils.NewWatch(newInner(data.Committee())),
	}
}

func (s *State) firstCommitQC() types.RoadIndex {
	for inner := range s.inner.Lock() {
		return inner.commitQCs.first
	}
	panic("unreachable")
}

// Data returns the data state.
func (s *State) Data() *data.State {
	return s.data
}

// LastCommitQC returns receiver of the LastCommitQC.
func (s *State) LastCommitQC() utils.AtomicRecv[utils.Option[*types.CommitQC]] {
	for inner := range s.inner.Lock() {
		return inner.latestCommitQC.Subscribe()
	}
	panic("unreachable")
}

func (s *State) waitForCommitQC(ctx context.Context, idx types.RoadIndex) error {
	_, err := s.LastCommitQC().Wait(ctx, func(qc utils.Option[*types.CommitQC]) bool {
		return types.NextIndexOpt(qc) > idx
	})
	return err
}

// LastAppQC returns the latest observed AppQC.
func (s *State) LastAppQC() utils.Option[*types.AppQC] {
	for inner := range s.inner.Lock() {
		return inner.latestAppQC
	}
	panic("unreachable")
}

// WaitForAppQC waits until there is an AppQC for the given index or higher.
// Returns this AppQC and the corresponding CommitQC.
// Together they provide enough information to prune the availability state.
func (s *State) WaitForAppQC(ctx context.Context, idx types.RoadIndex) (*types.AppQC, *types.CommitQC, error) {
	for inner, ctrl := range s.inner.Lock() {
		for {
			if appQC, ok := inner.latestAppQC.Get(); ok {
				if x := appQC.Proposal().RoadIndex(); x >= idx && inner.commitQCs.next > x {
					return appQC, inner.commitQCs.q[x], nil
				}
			}
			if err := ctrl.Wait(ctx); err != nil {
				return nil, nil, err
			}
		}
	}
	panic("unreachable")
}

// CommitQC returns the CommitQC for the given index.
func (s *State) CommitQC(ctx context.Context, idx types.RoadIndex) (*types.CommitQC, error) {
	if err := s.waitForCommitQC(ctx, idx); err != nil {
		return nil, err
	}
	for inner := range s.inner.Lock() {
		if idx < inner.commitQCs.first {
			return nil, data.ErrPruned
		}
		return inner.commitQCs.q[idx], nil
	}
	panic("unreachable")
}

// PushCommitQC pushes a CommitQC to the state.
// Waits until all previous CommitQCs are pushed.
func (s *State) PushCommitQC(ctx context.Context, qc *types.CommitQC) error {
	idx := qc.Proposal().Index()
	if idx > 0 {
		if err := s.waitForCommitQC(ctx, idx-1); err != nil {
			return err
		}
	}
	if err := qc.Verify(s.data.Committee()); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		if idx != inner.commitQCs.next {
			return nil
		}
		inner.commitQCs.pushBack(qc)
		inner.latestCommitQC.Store(utils.Some(qc))
		ctrl.Updated()
		return nil
	}
	return nil
}

// PushAppVote pushes an AppVote to the state.
func (s *State) PushAppVote(ctx context.Context, v *types.Signed[*types.AppVote]) error {
	if err := v.VerifySig(s.data.Committee()); err != nil {
		return fmt.Errorf("v.VerifySig(): %w", err)
	}
	idx := v.Msg().Proposal().RoadIndex()
	// Wait for the corresponding commitQC.
	if err := s.waitForCommitQC(ctx, idx); err != nil {
		return err
	}
	for inner, ctrl := range s.inner.Lock() {
		// Early exit if not useful (we collect <=1 AppQC per road index).
		if idx < types.NextOpt(inner.latestAppQC) {
			return nil
		}
		// Verify the vote against the CommitQC.
		qc := inner.commitQCs.q[idx]
		if err := v.Msg().Proposal().Verify(qc); err != nil {
			return fmt.Errorf("invalid vote: %w", err)
		}
		// Push the vote.
		n := v.Msg().Proposal().GlobalNumber()
		q := inner.appVotes
		for q.next <= n {
			q.pushBack(newAppVotes())
		}
		appQC, ok := q.q[n].pushVote(s.data.Committee(), v)
		if !ok {
			return nil
		}
		if inner.prune(appQC, qc) {
			ctrl.Updated()
		}
	}
	return nil
}

// PushAppQC pushes an AppQC to the state. It requires a corresponding CommitQC
// as a justification.
func (s *State) PushAppQC(appQC *types.AppQC, commitQC *types.CommitQC) error {
	// Check whether it is needed before verifying.
	for inner := range s.inner.Lock() {
		if types.NextOpt(inner.latestAppQC) > appQC.Proposal().RoadIndex() {
			return nil
		}
	}
	if err := appQC.Verify(s.data.Committee()); err != nil {
		return fmt.Errorf("appQC.Proposal().VerifyAgainstCommitQC(commitQC): %w", err)
	}
	if err := commitQC.Verify(s.data.Committee()); err != nil {
		return fmt.Errorf("commitQC.Verify(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		if inner.prune(appQC, commitQC) {
			ctrl.Updated()
		}
	}
	return nil
}

// NextBlock returns the index of the next missing block in local storage for the given lane.
func (s *State) NextBlock(lane types.LaneID) types.BlockNumber {
	for inner := range s.inner.Lock() {
		if l, ok := inner.blocks[lane]; ok {
			return l.next
		}
	}
	return 0
}

// Block returns block n of the given lane.
// Waits until the block is available.
// Returns ErrPruned if the block has been already pruned.
func (s *State) Block(ctx context.Context, lane types.LaneID, n types.BlockNumber) (*types.Block, error) {
	for inner, ctrl := range s.inner.Lock() {
		q, ok := inner.blocks[lane]
		if !ok {
			return nil, ErrBadLane
		}
		if err := ctrl.WaitUntil(ctx, func() bool { return n < q.next }); err != nil {
			return nil, err
		}
		if n < q.first {
			return nil, data.ErrPruned
		}
		return q.q[n], nil
	}
	panic("unreachable")
}

// PushBlock pushes a block to the state.
// Waits until all previous blocks are available.
func (s *State) PushBlock(ctx context.Context, p *types.Signed[*types.LaneProposal]) error {
	if err := p.Msg().Verify(s.data.Committee()); err != nil {
		return fmt.Errorf("block.Verify(): %w", err)
	}
	if err := p.VerifySig(s.data.Committee()); err != nil {
		return fmt.Errorf("p.VerifySig(): %w", err)
	}
	h := p.Msg().Block().Header()
	for inner, ctrl := range s.inner.Lock() {
		q, ok := inner.blocks[h.Lane()]
		if !ok {
			return ErrBadLane
		}
		if err := ctrl.WaitUntil(ctx, func() bool {
			return h.BlockNumber() <= min(q.next, q.first+blocksPerLane-1)
		}); err != nil {
			return err
		}
		// not needed any more
		if q.next != h.BlockNumber() {
			return nil
		}
		q.pushBack(p.Msg().Block())
		ctrl.Updated()
	}
	return nil
}

// PushVote pushes a LaneVote to the state.
// Waits until the lane has enough capacity for the new vote.
// It does NOT wait for the previous votes.
func (s *State) PushVote(ctx context.Context, vote *types.Signed[*types.LaneVote]) error {
	if err := vote.Msg().Verify(s.data.Committee()); err != nil {
		return fmt.Errorf("block.Verify(): %w", err)
	}
	if err := vote.VerifySig(s.data.Committee()); err != nil {
		return fmt.Errorf("p.VerifySig(): %w", err)
	}
	h := vote.Msg().Header()
	for inner, ctrl := range s.inner.Lock() {
		q, ok := inner.votes[h.Lane()]
		if !ok {
			return ErrBadLane
		}
		if err := ctrl.WaitUntil(ctx, func() bool {
			return h.BlockNumber() < q.first+blocksPerLane
		}); err != nil {
			return err
		}
		if h.BlockNumber() < q.first {
			return nil
		}
		for q.next <= h.BlockNumber() {
			q.pushBack(newBlockVotes())
		}
		if _, ok := q.q[h.BlockNumber()].pushVote(s.data.Committee(), vote); ok {
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
		q := inner.votes[lr.Lane()]
		for i := range headers {
			n := lr.Next() - types.BlockNumber(i) - 1
			for {
				// If pruned, then give up.
				if q.first > lr.First() {
					return nil, data.ErrPruned
				}
				// Check if we have the header.
				if vs := q.q[n].byHeader[want]; len(vs) > 0 {
					h := vs[0].Msg().Header()
					want = h.ParentHash()
					headers[len(headers)-i-1] = h
					break
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

func (s *State) fullCommitQC(ctx context.Context, n types.RoadIndex) (*types.FullCommitQC, error) {
	// Collect the CommitQC.
	qc, err := s.CommitQC(ctx, n)
	if err != nil {
		return nil, err
	}
	// Collect the headers from the votes.
	var commitHeaders []*types.BlockHeader
	for _, lane := range s.data.Committee().Lanes().All() {
		headers, err := s.headers(ctx, qc.LaneRange(lane))
		if err != nil {
			return nil, err
		}
		commitHeaders = append(commitHeaders, headers...)
	}
	return types.NewFullCommitQC(qc, commitHeaders), nil
}

// WaitForCapacity waits until the given lane has enough capacity for a new block.
func (s *State) WaitForCapacity(ctx context.Context, lane types.LaneID) error {
	for inner, ctrl := range s.inner.Lock() {
		q := inner.blocks[lane]
		if err := ctrl.WaitUntil(ctx, func() bool { return q.Len() < blocksPerLane }); err != nil {
			return err
		}
	}
	return nil
}

// WaitForLaneQCs waits until there is at least 1 LaneQC with a block not finalized by prev.
func (s *State) WaitForLaneQCs(
	ctx context.Context, prev utils.Option[*types.CommitQC],
) (map[types.LaneID]*types.LaneQC, error) {
	c := s.data.Committee()
	for inner, ctrl := range s.inner.Lock() {
		laneQCs := map[types.LaneID]*types.LaneQC{}
		for {
			for _, lane := range c.Lanes().All() {
				first := types.LaneRangeOpt(prev, lane).Next()
				for i := range types.BlockNumber(blocksPerLanePerCommit) {
					if qc, ok := inner.laneQC(c, lane, first+i); ok {
						laneQCs[lane] = qc
					} else {
						break
					}
				}
			}
			if len(laneQCs) > 0 {
				return laneQCs, nil
			}
			if err := ctrl.Wait(ctx); err != nil {
				return nil, err
			}
		}
	}
	panic("unreachable")
}

// ProduceBlock appends a new block to the given lane.
// Blocks until the lane has enough capacity for the new block.
func (s *State) ProduceBlock(ctx context.Context, lane types.LaneID, payload *types.Payload) (*types.Block, error) {
	for inner, ctrl := range s.inner.Lock() {
		q, ok := inner.blocks[lane]
		if !ok {
			return nil, ErrBadLane
		}
		if err := ctrl.WaitUntil(ctx, func() bool { return q.Len() < blocksPerLane }); err != nil {
			return nil, err
		}
		var parent types.BlockHeaderHash
		if q.first < q.next {
			parent = q.q[q.next-1].Header().Hash()
		}
		b := types.NewBlock(lane, q.next, parent, payload)
		q.q[q.next] = b
		q.next += 1
		ctrl.Updated()
		return b, nil
	}
	panic("unreachable")
}

// Run runs the background tasks of the state.
func (s *State) Run(ctx context.Context) error {
	return utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		// Task inserting FullCommitQCs and local blocks to data state.
		scope.SpawnNamed("s.data.PushQC", func() error {
			c := s.data.Committee()
			for n := types.RoadIndex(0); ; n = max(n+1, s.firstCommitQC()) {
				qc, err := s.fullCommitQC(ctx, n)
				if err != nil {
					if errors.Is(err, data.ErrPruned) {
						continue
					}
					return err
				}

				// Collect the blocks we have locally.
				var blocks []*types.Block
				for inner := range s.inner.Lock() {
					for _, lane := range c.Lanes().All() {
						lr := qc.QC().LaneRange(lane)
						for n := lr.First(); n < lr.Next(); n++ {
							// We are not expected to have all the blocks locally - only the available ones.
							if b, ok := inner.blocks[lr.Lane()].q[n]; ok {
								// We don't need to check the blocks against the headers,
								// as bad blocks will be filtered out by PushQC anyway.
								blocks = append(blocks, b)
							}
						}
					}
				}
				if err := s.data.PushQC(ctx, qc, blocks); err != nil {
					return fmt.Errorf("s.data.PushQC(): %w", err)
				}
			}
		})
		return nil
	}))
}
