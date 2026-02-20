package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func TestAppQC(keys []types.SecretKey, proposal *types.AppProposal) *types.AppQC {
	vote := types.NewAppVote(proposal)
	votes := make([]*types.Signed[*types.AppVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	return types.NewAppQC(votes)
}

func TestLaneQC(keys []types.SecretKey, header *types.BlockHeader) *types.LaneQC {
	vote := types.NewLaneVote(header)
	votes := make([]*types.Signed[*types.LaneVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	return types.NewLaneQC(votes)
}

func TestCommitQC(
	rng utils.Rng,
	committee *types.Committee,
	keys []types.SecretKey,
	prev utils.Option[*types.CommitQC],
) (*types.FullCommitQC, []*types.Block) {
	blocks := map[types.LaneID][]*types.Block{}
	makeBlock := func(producer types.LaneID) *types.Block {
		if bs := blocks[producer]; len(bs) > 0 {
			parent := bs[len(bs)-1]
			return types.NewBlock(
				producer,
				parent.Header().Next(),
				parent.Header().Hash(),
				types.GenPayload(rng),
			)
		}
		return types.NewBlock(
			producer,
			types.LaneRangeOpt(prev, producer).Next(),
			types.GenBlockHeaderHash(rng),
			types.GenPayload(rng),
		)
	}
	// Make some blocks
	for range 10 {
		producer := committee.Lanes().At(rng.Intn(committee.Lanes().Len()))
		blocks[producer] = append(blocks[producer], makeBlock(producer))
	}
	// Construct a proposal.
	laneQCs := map[types.LaneID]*types.LaneQC{}
	var headers []*types.BlockHeader
	var blockList []*types.Block
	for _, lane := range committee.Lanes().All() {
		if bs := blocks[lane]; len(bs) > 0 {
			laneQCs[lane] = TestLaneQC(keys, bs[len(bs)-1].Header())
			for _, b := range bs {
				headers = append(headers, b.Header())
				blockList = append(blockList, b)
			}
		}
	}
	viewSpec := types.ViewSpec{CommitQC: prev}
	leader := committee.Leader(viewSpec.View())
	var leaderKey types.SecretKey
	for _, k := range keys {
		if k.Public() == leader {
			leaderKey = k
			break
		}
	}
	proposal := utils.OrPanic1(types.NewProposal(
		leaderKey,
		committee,
		viewSpec,
		time.Now(),
		laneQCs,
		func() utils.Option[*types.AppQC] {
			if n := types.GlobalRangeOpt(prev).Next; n > 0 {
				p := types.NewAppProposal(n-1, viewSpec.View().Index, types.GenAppHash(rng))
				return utils.Some(TestAppQC(keys, p))
			}
			return utils.None[*types.AppQC]()
		}(),
	))
	votes := make([]*types.Signed[*types.CommitVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, types.Sign(k, types.NewCommitVote(proposal.Proposal().Msg())))
	}
	return types.NewFullCommitQC(
		types.NewCommitQC(votes),
		headers,
	), blockList
}

var _ StateAPI = (*MockState)(nil)

type innerMockState struct {
	blocks map[types.GlobalBlockNumber]*types.GlobalBlock // [first,next)
	first  types.GlobalBlockNumber
	next   types.GlobalBlockNumber
}

// MockState is a mock implementation of the StateAPI interface.
// Allows for pushing global blocks directly (without going through consensus).
type MockState struct {
	capacity uint64
	inner    utils.Watch[*innerMockState]
}

// NewMockState creates a new MockState with the given block capacity.
func NewMockState(capacity uint64) *MockState {
	return &MockState{
		capacity: capacity,
		inner: utils.NewWatch(&innerMockState{
			blocks: make(map[types.GlobalBlockNumber]*types.GlobalBlock),
			next:   0,
		}),
	}
}

// GlobalBlock returns the global block with the given number.
func (s *MockState) GlobalBlock(ctx context.Context, n types.GlobalBlockNumber) (*types.GlobalBlock, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return inner.next > n }); err != nil {
			return nil, err
		}
		if inner.first > n {
			return nil, ErrPruned
		}
		return inner.blocks[n], nil
	}
	panic("unreachable")
}

// ProduceBlock appends a new global block with the given payload.
func (s *MockState) ProduceBlock(ctx context.Context, payload *types.Payload) error {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			return uint64(inner.next-inner.first) < s.capacity
		}); err != nil {
			return err
		}
		inner.blocks[inner.next] = &types.GlobalBlock{
			GlobalNumber: inner.next,
			Payload:      payload,
		}
		inner.next += 1
		ctrl.Updated()
	}
	return nil
}

// PushAppHash marks all blocks up to n as executed.
func (s *MockState) PushAppHash(n types.GlobalBlockNumber, appHash types.AppHash) error {
	for inner, ctrl := range s.inner.Lock() {
		if got, wantMin := n, inner.first; got < wantMin {
			return fmt.Errorf("received app proposal out of order: got %v, want >= %v", got, wantMin)
		}
		if n >= inner.next {
			return errors.New("proposal for block which hasn't been received yet")
		}
		for inner.first <= n {
			delete(inner.blocks, inner.first)
			inner.first += 1
		}
		ctrl.Updated()
	}
	return nil
}

// Describe from prometheus.Collector.
func (s *MockState) Describe(chan<- *prometheus.Desc) {}

// Collect from prometheus.Collector.
func (s *MockState) Collect(chan<- prometheus.Metric) {}
