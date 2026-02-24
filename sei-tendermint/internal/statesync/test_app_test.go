package statesync

import (
	"context"
	"log"
	"sync"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

type Handler[V, R any] = func(context.Context, V) (R, error)

func mkHandler[V, R any](v V, r R) Handler[V, R] {
	return func(_ context.Context, got V) (R, error) {
		utils.OrPanic(utils.TestDiff(v, got))
		return r, nil
	}
}

type Queue[V, R any] struct {
	Handlers []Handler[V, R]
	Fallback Handler[V, R]
}

func (q *Queue[V, R]) Len() int             { return len(q.Handlers) }
func (q *Queue[V, R]) Set(v Handler[V, R])  { q.Fallback = v }
func (q *Queue[V, R]) Push(v Handler[V, R]) { q.Handlers = append(q.Handlers, v) }
func (q *Queue[V, R]) Pop() Handler[V, R] {
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

	offerSnapshot      Queue[*abci.RequestOfferSnapshot, *abci.ResponseOfferSnapshot]
	applySnapshotChunk Queue[*abci.RequestApplySnapshotChunk, *abci.ResponseApplySnapshotChunk]
	listSnapshots      Queue[*abci.RequestListSnapshots, *abci.ResponseListSnapshots]
	loadSnapshotChunk  Queue[*abci.RequestLoadSnapshotChunk, *abci.ResponseLoadSnapshotChunk]
	info               Queue[*abci.RequestInfo, *abci.ResponseInfo]
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
	log.Printf("QQQQQQ ListSnapshots()\n")
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

func (app *testStatesyncApp) Info(ctx context.Context, req *abci.RequestInfo) (*abci.ResponseInfo, error) {
	app.mu.Lock()
	h := app.info.Pop()
	app.mu.Unlock()
	return h(ctx, req)
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
