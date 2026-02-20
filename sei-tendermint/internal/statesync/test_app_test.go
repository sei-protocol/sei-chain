package statesync

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

type offerSnapshotHandler func(context.Context, *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error)
type applySnapshotChunkHandler func(context.Context, *abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error)
type listSnapshotsHandler func(context.Context, *abci.RequestListSnapshots) (*abci.ResponseListSnapshots, error)
type loadSnapshotChunkHandler func(context.Context, *abci.RequestLoadSnapshotChunk) (*abci.ResponseLoadSnapshotChunk, error)
type infoHandler func(context.Context, *abci.RequestInfo) (*abci.ResponseInfo, error)

type Handler[V,R any] = func(context.Context,V) (R,error)

func mkHandler[V,R any](t *testing.T, v V, r R) Handler[V,R] {
	return func(_ context.Context, got V) (R,error) {
		require.Equal(t,got,v)
		return r,nil
	}
}

type Queue[V,R any] struct {
	Handlers []Handler[V,R]
	Fallback Handler[V,R]
}

func (q *Queue[V,R]) Len() int { return len(q.Handlers) }
func (q *Queue[V,R]) Set(v Handler[V,R]) { q.Fallback = v }
func (q *Queue[V,R]) Push(v Handler[V,R]) { q.Handlers = append(q.Handlers,v) }
func (q *Queue[V,R]) Pop() Handler[V,R] {
	if len(q.Handlers)>0 {
		res := q.Handlers[0]
		q.Handlers = q.Handlers[1:]
		return res
	}
	return q.Fallback
}

type testStatesyncApp struct {
	*abci.BaseApplication
	mu sync.Mutex

	offerSnapshot Queue[*abci.RequestOfferSnapshot, *abci.ResponseOfferSnapshot]
	applySnapshotChunk Queue[*abci.RequestApplySnapshotChunk, *abci.ResponseApplySnapshotChunk]
	listSnapshots Queue[*abci.RequestListSnapshots,*abci.ResponseListSnapshots]
	loadSnapshotChunk  Queue[*abci.RequestLoadSnapshotChunk,*abci.ResponseLoadSnapshotChunk]
	info Queue[*abci.RequestInfo,*abci.ResponseInfo]
}

func newTestStatesyncApp(t testing.TB) *testStatesyncApp {
	return &testStatesyncApp{BaseApplication: abci.NewBaseApplication()}
}

func mustTestStatesyncApp(t testing.TB, app abci.Application) *testStatesyncApp {
	t.Helper()
	ta, ok := app.(*testStatesyncApp)
	require.True(t, ok, "expected *testStatesyncApp, got %T", app)
	return ta
}

func (app *testStatesyncApp) OfferSnapshot(ctx context.Context, req *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
	app.mu.Lock()
	h := app.offerSnapshot.Pop()
	app.mu.Unlock()
	return h(ctx,req)
}

func (app *testStatesyncApp) ApplySnapshotChunk(ctx context.Context, req *abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error) {
	app.mu.Lock()
	h := app.applySnapshotChunk.Pop()
	app.mu.Unlock()
	return h(ctx,req)
}

func (app *testStatesyncApp) ListSnapshots(ctx context.Context, req *abci.RequestListSnapshots) (*abci.ResponseListSnapshots, error) {
	app.mu.Lock()
	h := app.listSnapshots.Pop()
	app.mu.Unlock()
	return h(ctx,req)
}

func (app *testStatesyncApp) LoadSnapshotChunk(ctx context.Context, req *abci.RequestLoadSnapshotChunk) (*abci.ResponseLoadSnapshotChunk, error) {
	app.mu.Lock()
	h := app.loadSnapshotChunk.Pop()
	app.mu.Unlock()
	return h(ctx,req)
}

func (app *testStatesyncApp) Info(ctx context.Context, req *abci.RequestInfo) (*abci.ResponseInfo, error) {
	app.mu.Lock()
	h := app.info.Pop()
	app.mu.Unlock()
	return h(ctx,req)
}

func (app *testStatesyncApp) AssertExpectations(t testing.TB) {
	app.mu.Lock()
	defer app.mu.Unlock()
	require.Equal(t, app.offerSnapshot.Len(), 0, "pending OfferSnapshot expectations")
	require.Equal(t, app.applySnapshotChunk.Len(), 0, "pending ApplySnapshotChunk expectations")
	require.Equal(t, app.listSnapshots.Len(), 0, "pending ListSnapshots expectations")
	require.Equal(t, app.loadSnapshotChunk.Len(), 0, "pending LoadSnapshotChunk expectations")
	require.Equal(t, app.info.Len(), 0, "pending Info expectations")
}

