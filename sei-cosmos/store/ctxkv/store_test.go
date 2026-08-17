package ctxkv_test

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/ctxkv"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
)

type recordingStore struct {
	stubIterStore
	gotCtx context.Context
}

func (s *recordingStore) IteratorWithContext(ctx context.Context, start, end []byte) types.Iterator {
	s.gotCtx = ctx
	return s.Iterator(start, end)
}

func (s *recordingStore) ReverseIteratorWithContext(ctx context.Context, start, end []byte) types.Iterator {
	s.gotCtx = ctx
	return s.ReverseIterator(start, end)
}

func TestWrapIsNoopWhenContextCannotCancel(t *testing.T) {
	parent := &recordingStore{}
	require.Equal(t, parent, ctxkv.Wrap(parent, nil))
	require.Equal(t, parent, ctxkv.Wrap(parent, context.Background()))
}

func TestWrapForwardsDeadlineToContextIterator(t *testing.T) {
	parent := &recordingStore{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapped := ctxkv.Wrap(parent, ctx)
	require.NotEqual(t, parent, wrapped)

	_ = wrapped.Iterator(nil, nil)
	require.Equal(t, ctx, parent.gotCtx)

	parent.gotCtx = nil
	_ = wrapped.ReverseIterator(nil, nil)
	require.Equal(t, ctx, parent.gotCtx)
}

type stubIterStore struct{}

func (stubIterStore) GetStoreType() types.StoreType { return types.StoreTypeDB }
func (stubIterStore) CacheWrap(types.StoreKey) types.CacheWrap {
	panic("not implemented")
}
func (stubIterStore) CacheWrapWithTrace(types.StoreKey, io.Writer, types.TraceContext) types.CacheWrap {
	panic("not implemented")
}
func (stubIterStore) Get([]byte) []byte  { return nil }
func (stubIterStore) Has([]byte) bool    { return false }
func (stubIterStore) Set([]byte, []byte) {}
func (stubIterStore) Delete([]byte)      {}
func (stubIterStore) Iterator(start, end []byte) types.Iterator {
	return &emptyIter{start: start, end: end}
}
func (stubIterStore) ReverseIterator(start, end []byte) types.Iterator {
	return &emptyIter{start: start, end: end}
}
func (stubIterStore) GetWorkingHash() ([]byte, error) { return nil, nil }
func (stubIterStore) VersionExists(int64) bool        { return true }
func (stubIterStore) DeleteAll([]byte, []byte) error  { return nil }
func (stubIterStore) GetAllKeyStrsInRange([]byte, []byte) []string {
	return nil
}

type emptyIter struct {
	start, end []byte
}

func (e *emptyIter) Domain() ([]byte, []byte) { return e.start, e.end }
func (e *emptyIter) Valid() bool              { return false }
func (e *emptyIter) Next()                    {}
func (e *emptyIter) Key() []byte              { return nil }
func (e *emptyIter) Value() []byte            { return nil }
func (e *emptyIter) Error() error             { return nil }
func (e *emptyIter) Close() error             { return nil }
