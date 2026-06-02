package migration

import (
	"errors"
	"testing"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

// recordingInnerRouter is a fake Router used to test the flushingRouter
// wrapper: it records ApplyChangeSets calls and read-method dispatch and can be
// configured to fail the apply.
type recordingInnerRouter struct {
	applyCount    int
	applyErr      error
	lastReadKey   []byte
	readValue     []byte
	readFound     bool
	lastIterStore string
	lastProofKey  []byte
}

var _ Router = (*recordingInnerRouter)(nil)

func (r *recordingInnerRouter) ApplyChangeSets(_ []*proto.NamedChangeSet, _ bool) error {
	r.applyCount++
	return r.applyErr
}

func (r *recordingInnerRouter) Read(store string, key []byte) ([]byte, bool, error) {
	r.lastReadKey = key
	return r.readValue, r.readFound, nil
}

func (r *recordingInnerRouter) Iterator(store string, _ []byte, _ []byte, _ bool) (dbm.Iterator, error) {
	r.lastIterStore = store
	return nil, nil
}

func (r *recordingInnerRouter) GetProof(_ string, key []byte) (*ics23.CommitmentProof, error) {
	r.lastProofKey = key
	return nil, nil
}

func TestFlushingRouter_RunsCallbacksAfterApplyInOrder(t *testing.T) {
	inner := &recordingInnerRouter{}
	var order []string
	f := newFlushingRouter(inner,
		func() error { order = append(order, "first"); return nil },
		func() error { order = append(order, "second"); return nil },
	)

	require.NoError(t, f.ApplyChangeSets(nil, true))
	require.Equal(t, 1, inner.applyCount)
	require.Equal(t, []string{"first", "second"}, order, "callbacks must run in order after the inner apply")
}

func TestFlushingRouter_InnerErrorShortCircuitsCallbacks(t *testing.T) {
	wantErr := errors.New("inner boom")
	inner := &recordingInnerRouter{applyErr: wantErr}
	ran := false
	f := newFlushingRouter(inner, func() error { ran = true; return nil })

	require.ErrorIs(t, f.ApplyChangeSets(nil, true), wantErr)
	require.False(t, ran, "callbacks must not run when the inner apply fails")
}

func TestFlushingRouter_CallbackErrorPropagates(t *testing.T) {
	inner := &recordingInnerRouter{}
	wantErr := errors.New("flush boom")
	secondRan := false
	f := newFlushingRouter(inner,
		func() error { return wantErr },
		func() error { secondRan = true; return nil },
	)

	err := f.ApplyChangeSets(nil, true)
	require.ErrorIs(t, err, wantErr)
	require.True(t, secondRan, "all callbacks run; errors are joined, not short-circuited")
}

func TestFlushingRouter_ReadMethodsDelegate(t *testing.T) {
	inner := &recordingInnerRouter{readValue: []byte("v"), readFound: true}
	f := newFlushingRouter(inner)

	val, found, err := f.Read("evm", []byte("k"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v"), val)
	require.Equal(t, []byte("k"), inner.lastReadKey)

	_, err = f.Iterator("evm", []byte("s"), []byte("e"), true)
	require.NoError(t, err)
	require.Equal(t, "evm", inner.lastIterStore)

	_, err = f.GetProof("evm", []byte("pk"))
	require.NoError(t, err)
	require.Equal(t, []byte("pk"), inner.lastProofKey)
}

// TestFlushingRouter_CoalescesSharedAccumulator is a direct-topology test that
// proves both-backend coalescing without going through BuildRouter: two routes
// share one accumulating writer per backend, so a single dispatch yields
// exactly one downstream call per backend even though each backend is targeted
// by two routes.
func TestFlushingRouter_CoalescesSharedAccumulator(t *testing.T) {
	flatKVLeaf := &recordingWriter{}
	memIAVLLeaf := &recordingWriter{}
	flatKVAcc := newAccumulatingWriter(flatKVLeaf.write)
	memIAVLAcc := newAccumulatingWriter(memIAVLLeaf.write)

	// Two routes target flatKV (evm/, other/) and two target memIAVL (bank/, aux/),
	// mirroring the MigrateAllButBank fan-out shape.
	evmRoute, err := NewRoute(flushTestStubReader, flatKVAcc.Apply, nil, nil, "evm")
	require.NoError(t, err)
	otherRoute, err := NewRoute(flushTestStubReader, flatKVAcc.Apply, nil, nil, "other")
	require.NoError(t, err)
	bankRoute, err := NewRoute(flushTestStubReader, memIAVLAcc.Apply, nil, nil, "bank")
	require.NoError(t, err)
	auxRoute, err := NewRoute(flushTestStubReader, memIAVLAcc.Apply, nil, nil, "aux")
	require.NoError(t, err)

	moduleRouter, err := NewModuleRouter(evmRoute, otherRoute, bankRoute, auxRoute)
	require.NoError(t, err)
	router := newFlushingRouter(moduleRouter, memIAVLAcc.Flush, flatKVAcc.Flush)

	require.NoError(t, router.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("evm", kv("e", "1")),
		namedCS("other", kv("o", "2")),
		namedCS("bank", kv("b", "3")),
		namedCS("aux", kv("a", "4")),
	}, true))

	require.Len(t, flatKVLeaf.calls, 1, "flatKV must be written exactly once per dispatch")
	require.Len(t, memIAVLLeaf.calls, 1, "memIAVL must be written exactly once per dispatch")
	require.Equal(t, []*proto.NamedChangeSet{namedCS("evm", kv("e", "1")), namedCS("other", kv("o", "2"))},
		flatKVLeaf.calls[0].changesets)
	require.Equal(t, []*proto.NamedChangeSet{namedCS("bank", kv("b", "3")), namedCS("aux", kv("a", "4"))},
		memIAVLLeaf.calls[0].changesets)
}

func flushTestStubReader(string, []byte) ([]byte, bool, error) { return nil, false, nil }
