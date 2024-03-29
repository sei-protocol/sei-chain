package tasks

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/cosmos/cosmos-sdk/store/cachekv"
	"github.com/cosmos/cosmos-sdk/store/cachemulti"
	"github.com/cosmos/cosmos-sdk/store/dbadapter"
	"github.com/cosmos/cosmos-sdk/store/multiversion"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/occ"
	"github.com/cosmos/cosmos-sdk/utils/tracing"
)

type mockDeliverTxFunc func(ctx sdk.Context, req types.RequestDeliverTx, tx sdk.Tx, checksum [32]byte) (res types.ResponseDeliverTx)

var testStoreKey = sdk.NewKVStoreKey("mock")
var itemKey = []byte("key")

func requestList(n int) []*sdk.DeliverTxEntry {
	tasks := make([]*sdk.DeliverTxEntry, n)
	for i := 0; i < n; i++ {
		tasks[i] = &sdk.DeliverTxEntry{
			Request: types.RequestDeliverTx{
				Tx: []byte(fmt.Sprintf("%d", i)),
			},
			AbsoluteIndex: i,
			// TODO: maybe we need to add dummy sdkTx message types and handler routers too
		}

	}
	return tasks
}

func abortRecoveryFunc(response *types.ResponseDeliverTx) {
	if r := recover(); r != nil {
		_, ok := r.(occ.Abort)
		if !ok {
			panic(r)
		}
		// empty code and codespace
		response.Info = "occ abort"
	}
}

func requestListWithEstimatedWritesets(n int) []*sdk.DeliverTxEntry {
	tasks := make([]*sdk.DeliverTxEntry, n)
	for i := 0; i < n; i++ {
		tasks[i] = &sdk.DeliverTxEntry{
			Request: types.RequestDeliverTx{
				Tx: []byte(fmt.Sprintf("%d", i)),
			},
			AbsoluteIndex: i,
			EstimatedWritesets: sdk.MappedWritesets{
				testStoreKey: multiversion.WriteSet{
					string(itemKey): []byte("foo"),
				},
			},
		}

	}
	return tasks
}

func initTestCtx(injectStores bool) sdk.Context {
	ctx := sdk.Context{}.WithContext(context.Background())
	keys := make(map[string]sdk.StoreKey)
	stores := make(map[sdk.StoreKey]sdk.CacheWrapper)
	db := dbm.NewMemDB()
	if injectStores {
		mem := dbadapter.Store{DB: db}
		stores[testStoreKey] = cachekv.NewStore(mem, testStoreKey, 1000)
		keys[testStoreKey.Name()] = testStoreKey
	}
	store := cachemulti.NewStore(db, stores, keys, nil, nil, nil)
	ctx = ctx.WithMultiStore(&store)
	ctx = ctx.WithLogger(log.NewNopLogger())
	return ctx
}

func TestProcessAll(t *testing.T) {
	runtime.SetBlockProfileRate(1)

	go func() {
		http.ListenAndServe("localhost:6060", nil)
	}()

	tests := []struct {
		name          string
		workers       int
		runs          int
		before        func(ctx sdk.Context)
		requests      []*sdk.DeliverTxEntry
		deliverTxFunc mockDeliverTxFunc
		addStores     bool
		expectedErr   error
		assertions    func(t *testing.T, ctx sdk.Context, res []types.ResponseDeliverTx)
	}{
		{
			name:      "Test zero txs does not hang",
			workers:   20,
			runs:      10,
			addStores: true,
			requests:  requestList(0),
			deliverTxFunc: func(ctx sdk.Context, req types.RequestDeliverTx, tx sdk.Tx, checksum [32]byte) (res types.ResponseDeliverTx) {
				panic("should not deliver")
			},
			assertions: func(t *testing.T, ctx sdk.Context, res []types.ResponseDeliverTx) {
				require.Len(t, res, 0)
			},
			expectedErr: nil,
		},
		{
			name:      "Test tx writing to a store that another tx is iterating",
			workers:   50,
			runs:      1,
			requests:  requestList(100),
			addStores: true,
			before: func(ctx sdk.Context) {
				kv := ctx.MultiStore().GetKVStore(testStoreKey)
				// initialize 100 test values in the base kv store so iterating isn't too fast
				for i := 0; i < 10; i++ {
					kv.Set([]byte(fmt.Sprintf("%d", i)), []byte(fmt.Sprintf("%d", i)))
				}
			},
			deliverTxFunc: func(ctx sdk.Context, req types.RequestDeliverTx, tx sdk.Tx, checksum [32]byte) (res types.ResponseDeliverTx) {
				defer abortRecoveryFunc(&res)
				kv := ctx.MultiStore().GetKVStore(testStoreKey)
				if ctx.TxIndex()%2 == 0 {
					// For even-indexed transactions, write to the store
					kv.Set(req.Tx, req.Tx)
					return types.ResponseDeliverTx{
						Info: "write",
					}
				} else {
					// For odd-indexed transactions, iterate over the store

					// just write so we have more writes going on
					kv.Set(req.Tx, req.Tx)
					iterator := kv.Iterator(nil, nil)
					defer iterator.Close()
					for ; iterator.Valid(); iterator.Next() {
						// Do nothing, just iterate
					}
					return types.ResponseDeliverTx{
						Info: "iterate",
					}
				}
			},
			assertions: func(t *testing.T, ctx sdk.Context, res []types.ResponseDeliverTx) {
				for idx, response := range res {
					if idx%2 == 0 {
						require.Equal(t, "write", response.Info)
					} else {
						require.Equal(t, "iterate", response.Info)
					}
				}
			},
			expectedErr: nil,
		},
		{
			name:      "Test no overlap txs",
			workers:   20,
			runs:      10,
			addStores: true,
			requests:  requestList(1000),
			deliverTxFunc: func(ctx sdk.Context, req types.RequestDeliverTx, tx sdk.Tx, checksum [32]byte) (res types.ResponseDeliverTx) {
				defer abortRecoveryFunc(&res)
				// all txs read and write to the same key to maximize conflicts
				kv := ctx.MultiStore().GetKVStore(testStoreKey)

				// write to the store with this tx's index
				kv.Set(req.Tx, req.Tx)
				val := string(kv.Get(req.Tx))

				// return what was read from the store (final attempt should be index-1)
				return types.ResponseDeliverTx{
					Info: val,
				}
			},
			assertions: func(t *testing.T, ctx sdk.Context, res []types.ResponseDeliverTx) {
				for idx, response := range res {
					require.Equal(t, fmt.Sprintf("%d", idx), response.Info)
				}
				store := ctx.MultiStore().GetKVStore(testStoreKey)
				for i := 0; i < len(res); i++ {
					val := store.Get([]byte(fmt.Sprintf("%d", i)))
					require.Equal(t, []byte(fmt.Sprintf("%d", i)), val)
				}
			},
			expectedErr: nil,
		},
		{
			name:      "Test every tx accesses same key",
			workers:   50,
			runs:      5,
			addStores: true,
			requests:  requestList(1000),
			deliverTxFunc: func(ctx sdk.Context, req types.RequestDeliverTx, tx sdk.Tx, checksum [32]byte) (res types.ResponseDeliverTx) {
				defer abortRecoveryFunc(&res)
				// all txs read and write to the same key to maximize conflicts
				kv := ctx.MultiStore().GetKVStore(testStoreKey)
				val := string(kv.Get(itemKey))

				// write to the store with this tx's index
				kv.Set(itemKey, req.Tx)

				// return what was read from the store (final attempt should be index-1)
				return types.ResponseDeliverTx{
					Info: val,
				}
			},
			assertions: func(t *testing.T, ctx sdk.Context, res []types.ResponseDeliverTx) {
				for idx, response := range res {
					if idx == 0 {
						require.Equal(t, "", response.Info)
					} else {
						// the info is what was read from the kv store by the tx
						// each tx writes its own index, so the info should be the index of the previous tx
						require.Equal(t, fmt.Sprintf("%d", idx-1), response.Info)
					}
				}
				// confirm last write made it to the parent store
				latest := ctx.MultiStore().GetKVStore(testStoreKey).Get(itemKey)
				require.Equal(t, []byte(fmt.Sprintf("%d", len(res)-1)), latest)
			},
			expectedErr: nil,
		},
		{
			name:      "Test every tx accesses same key with estimated writesets",
			workers:   50,
			runs:      1,
			addStores: true,
			requests:  requestListWithEstimatedWritesets(1000),
			deliverTxFunc: func(ctx sdk.Context, req types.RequestDeliverTx, tx sdk.Tx, checksum [32]byte) (res types.ResponseDeliverTx) {
				defer abortRecoveryFunc(&res)
				// all txs read and write to the same key to maximize conflicts
				kv := ctx.MultiStore().GetKVStore(testStoreKey)
				val := string(kv.Get(itemKey))

				// write to the store with this tx's index
				kv.Set(itemKey, req.Tx)

				// return what was read from the store (final attempt should be index-1)
				return types.ResponseDeliverTx{
					Info: val,
				}
			},
			assertions: func(t *testing.T, ctx sdk.Context, res []types.ResponseDeliverTx) {
				for idx, response := range res {
					if idx == 0 {
						require.Equal(t, "", response.Info)
					} else {
						// the info is what was read from the kv store by the tx
						// each tx writes its own index, so the info should be the index of the previous tx
						require.Equal(t, fmt.Sprintf("%d", idx-1), response.Info)
					}
				}
				// confirm last write made it to the parent store
				latest := ctx.MultiStore().GetKVStore(testStoreKey).Get(itemKey)
				require.Equal(t, []byte(fmt.Sprintf("%d", len(res)-1)), latest)
			},
			expectedErr: nil,
		},
		{
			name:      "Test some tx accesses same key",
			workers:   50,
			runs:      1,
			addStores: true,
			requests:  requestList(2000),
			deliverTxFunc: func(ctx sdk.Context, req types.RequestDeliverTx, tx sdk.Tx, checksum [32]byte) (res types.ResponseDeliverTx) {
				defer abortRecoveryFunc(&res)
				if ctx.TxIndex()%10 != 0 {
					return types.ResponseDeliverTx{
						Info: "none",
					}
				}
				// all txs read and write to the same key to maximize conflicts
				kv := ctx.MultiStore().GetKVStore(testStoreKey)
				val := string(kv.Get(itemKey))

				// write to the store with this tx's index
				kv.Set(itemKey, req.Tx)

				// return what was read from the store (final attempt should be index-1)
				return types.ResponseDeliverTx{
					Info: val,
				}
			},
			assertions:  func(t *testing.T, ctx sdk.Context, res []types.ResponseDeliverTx) {},
			expectedErr: nil,
		},
		{
			name:      "Test no stores on context should not panic",
			workers:   50,
			runs:      10,
			addStores: false,
			requests:  requestList(10),
			deliverTxFunc: func(ctx sdk.Context, req types.RequestDeliverTx, tx sdk.Tx, checksum [32]byte) (res types.ResponseDeliverTx) {
				defer abortRecoveryFunc(&res)
				return types.ResponseDeliverTx{
					Info: fmt.Sprintf("%d", ctx.TxIndex()),
				}
			},
			assertions: func(t *testing.T, ctx sdk.Context, res []types.ResponseDeliverTx) {
				for idx, response := range res {
					require.Equal(t, fmt.Sprintf("%d", idx), response.Info)
				}
			},
			expectedErr: nil,
		},
		{
			name:      "Test every tx accesses same key with estimated writesets",
			workers:   50,
			runs:      1,
			addStores: true,
			requests:  requestListWithEstimatedWritesets(1000),
			deliverTxFunc: func(ctx sdk.Context, req types.RequestDeliverTx, tx sdk.Tx, checksum [32]byte) (res types.ResponseDeliverTx) {
				defer abortRecoveryFunc(&res)
				// all txs read and write to the same key to maximize conflicts
				kv := ctx.MultiStore().GetKVStore(testStoreKey)
				val := string(kv.Get(itemKey))

				// write to the store with this tx's index
				kv.Set(itemKey, req.Tx)

				// return what was read from the store (final attempt should be index-1)
				return types.ResponseDeliverTx{
					Info: val,
				}
			},
			assertions: func(t *testing.T, ctx sdk.Context, res []types.ResponseDeliverTx) {
				for idx, response := range res {
					if idx == 0 {
						require.Equal(t, "", response.Info)
					} else {
						// the info is what was read from the kv store by the tx
						// each tx writes its own index, so the info should be the index of the previous tx
						require.Equal(t, fmt.Sprintf("%d", idx-1), response.Info)
					}
				}
				// confirm last write made it to the parent store
				latest := ctx.MultiStore().GetKVStore(testStoreKey).Get(itemKey)
				require.Equal(t, []byte(fmt.Sprintf("%d", len(res)-1)), latest)
			},
			expectedErr: nil,
		},
		{
			name:      "Test every tx accesses same key with delays",
			workers:   50,
			runs:      1,
			addStores: true,
			requests:  requestList(1000),
			deliverTxFunc: func(ctx sdk.Context, req types.RequestDeliverTx, tx sdk.Tx, checksum [32]byte) (res types.ResponseDeliverTx) {
				defer abortRecoveryFunc(&res)
				wait := rand.Intn(10)
				time.Sleep(time.Duration(wait) * time.Millisecond)
				// all txs read and write to the same key to maximize conflicts
				kv := ctx.MultiStore().GetKVStore(testStoreKey)
				val := string(kv.Get(itemKey))
				time.Sleep(time.Duration(wait) * time.Millisecond)
				// write to the store with this tx's index
				newVal := val + fmt.Sprintf("%d", ctx.TxIndex())
				kv.Set(itemKey, []byte(newVal))

				// return what was read from the store (final attempt should be index-1)
				return types.ResponseDeliverTx{
					Info: newVal,
				}
			},
			assertions: func(t *testing.T, ctx sdk.Context, res []types.ResponseDeliverTx) {
				expected := ""
				for idx, response := range res {
					expected = expected + fmt.Sprintf("%d", idx)
					require.Equal(t, expected, response.Info)
				}
				// confirm last write made it to the parent store
				latest := ctx.MultiStore().GetKVStore(testStoreKey).Get(itemKey)
				require.Equal(t, expected, string(latest))
			},
			expectedErr: nil,
		},
		{
			name:      "Test tx Reset properly before re-execution via tracer",
			workers:   10,
			runs:      1,
			addStores: true,
			requests:  addTxTracerToTxEntries(requestList(250)),
			deliverTxFunc: func(ctx sdk.Context, req types.RequestDeliverTx, tx sdk.Tx, checksum [32]byte) (res types.ResponseDeliverTx) {
				defer abortRecoveryFunc(&res)
				wait := rand.Intn(10)
				time.Sleep(time.Duration(wait) * time.Millisecond)
				// all txs read and write to the same key to maximize conflicts
				kv := ctx.MultiStore().GetKVStore(testStoreKey)
				val := string(kv.Get(itemKey))
				time.Sleep(time.Duration(wait) * time.Millisecond)
				// write to the store with this tx's index
				newVal := val + fmt.Sprintf("%d", ctx.TxIndex())
				kv.Set(itemKey, []byte(newVal))

				if v, ok := ctx.Context().Value("test_tracer").(*testTxTracer); ok {
					v.OnTxExecute()
				}

				// return what was read from the store (final attempt should be index-1)
				return types.ResponseDeliverTx{
					Info: newVal,
				}
			},
			assertions: func(t *testing.T, ctx sdk.Context, res []types.ResponseDeliverTx) {
				expected := ""
				for idx, response := range res {
					expected = expected + fmt.Sprintf("%d", idx)
					require.Equal(t, expected, response.Info)
				}
				// confirm last write made it to the parent store
				latest := ctx.MultiStore().GetKVStore(testStoreKey).Get(itemKey)
				require.Equal(t, expected, string(latest))
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < tt.runs; i++ {
				// set a tracer provider
				tp := trace.NewNoopTracerProvider()
				otel.SetTracerProvider(trace.NewNoopTracerProvider())
				tr := tp.Tracer("scheduler-test")
				ti := &tracing.Info{
					Tracer: &tr,
				}

				s := NewScheduler(tt.workers, ti, tt.deliverTxFunc)
				ctx := initTestCtx(tt.addStores)

				if tt.before != nil {
					tt.before(ctx)
				}

				res, err := s.ProcessAll(ctx, tt.requests)
				require.LessOrEqual(t, s.(*scheduler).maxIncarnation, maximumIterations)
				require.Len(t, res, len(tt.requests))

				if !errors.Is(err, tt.expectedErr) {
					t.Errorf("Expected error %v, got %v", tt.expectedErr, err)
				} else {
					tt.assertions(t, ctx, res)
				}
			}
		})
	}
}

func addTxTracerToTxEntries(txEntries []*sdk.DeliverTxEntry) []*sdk.DeliverTxEntry {
	for _, txEntry := range txEntries {
		txEntry.TxTracer = newTestTxTracer(txEntry.AbsoluteIndex)
	}

	return txEntries
}

var _ sdk.TxTracer = &testTxTracer{}

func newTestTxTracer(txIndex int) *testTxTracer {
	return &testTxTracer{txIndex: txIndex, canExecute: true}
}

type testTxTracer struct {
	txIndex    int
	canExecute bool
}

func (t *testTxTracer) Commit() {
	t.canExecute = false
}

func (t *testTxTracer) InjectInContext(ctx sdk.Context) sdk.Context {
	return ctx.WithContext(context.WithValue(ctx.Context(), "test_tracer", t))
}

func (t *testTxTracer) Reset() {
	t.canExecute = true
}

func (t *testTxTracer) OnTxExecute() {
	if !t.canExecute {
		panic(fmt.Errorf("task #%d was asked to execute but the tracer is not in the correct state, most probably due to missing Reset call or over execution", t.txIndex))
	}

	t.canExecute = false
}
