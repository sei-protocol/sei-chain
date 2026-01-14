package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/internal/autobahn/types"
)

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
