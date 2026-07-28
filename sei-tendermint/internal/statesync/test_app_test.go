package statesync

import (
	"context"
	"sync"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

type Handler[V, R any] = func(context.Context, V) (R, error)

func mkConst[R any](r R) func() R {
	return func() R { return r }
}

func mkHandler[V, R any](v V, r R) Handler[V, R] {
	return func(_ context.Context, got V) (R, error) {
		utils.OrPanic(utils.TestDiff(v, got))
		return r, nil
	}
}

type Queue[T any] struct {
	Handlers []T
	Fallback T
}

func (q *Queue[T]) Len() int { return len(q.Handlers) }
func (q *Queue[T]) Set(v T)  { q.Fallback = v }
func (q *Queue[T]) Push(v T) { q.Handlers = append(q.Handlers, v) }
func (q *Queue[T]) Pop() T {
	if len(q.Handlers) > 0 {
		res := q.Handlers[0]
		q.Handlers = q.Handlers[1:]
		return res
	}
	return q.Fallback
}

type testStatesyncApp struct {
	*abci.BaseApplication
	mu sync.Mutex

	offerSnapshot      Queue[Handler[*abci.RequestOfferSnapshot, *abci.ResponseOfferSnapshot]]
	applySnapshotChunk Queue[Handler[*abci.RequestApplySnapshotChunk, *abci.ResponseApplySnapshotChunk]]
	listSnapshots      Queue[Handler[*abci.RequestListSnapshots, *abci.ResponseListSnapshots]]
	loadSnapshotChunk  Queue[Handler[*abci.RequestLoadSnapshotChunk, *abci.ResponseLoadSnapshotChunk]]
	info               Queue[func() *abci.ResponseInfo]
}

func newTestStatesyncApp() *testStatesyncApp {
	return &testStatesyncApp{}
}

func (app *testStatesyncApp) OfferSnapshot(ctx context.Context, req *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
	app.mu.Lock()
	h := app.offerSnapshot.Pop()
	app.mu.Unlock()
	return h(ctx, req)
}

func (app *testStatesyncApp) ApplySnapshotChunk(ctx context.Context, req *abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error) {
	app.mu.Lock()
	h := app.applySnapshotChunk.Pop()
	app.mu.Unlock()
	return h(ctx, req)
}

func (app *testStatesyncApp) ListSnapshots(ctx context.Context, req *abci.RequestListSnapshots) (*abci.ResponseListSnapshots, error) {
	app.mu.Lock()
	h := app.listSnapshots.Pop()
	app.mu.Unlock()
	return h(ctx, req)
}

func (app *testStatesyncApp) LoadSnapshotChunk(ctx context.Context, req *abci.RequestLoadSnapshotChunk) (*abci.ResponseLoadSnapshotChunk, error) {
	app.mu.Lock()
	h := app.loadSnapshotChunk.Pop()
	app.mu.Unlock()
	return h(ctx, req)
}

func (app *testStatesyncApp) Info() *abci.ResponseInfo {
	app.mu.Lock()
	h := app.info.Pop()
	app.mu.Unlock()
	return h()
}

func (app *testStatesyncApp) AssertExpectations(t testing.TB) {
	app.mu.Lock()
	defer app.mu.Unlock()
	require.Equal(t, 0, app.offerSnapshot.Len(), "pending OfferSnapshot expectations")
	require.Equal(t, 0, app.applySnapshotChunk.Len(), "pending ApplySnapshotChunk expectations")
	require.Equal(t, 0, app.listSnapshots.Len(), "pending ListSnapshots expectations")
	require.Equal(t, 0, app.loadSnapshotChunk.Len(), "pending LoadSnapshotChunk expectations")
	require.Equal(t, 0, app.info.Len(), "pending Info expectations")
}
