package migration

import (
	"errors"
	"testing"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// newPassthroughRouterForTest wires the standard mockDB reader/writer
// into a PassthroughRouter with a nil proof builder. Tests that need
// proofs construct the router directly.
func newPassthroughRouterForTest(t *testing.T) (*PassthroughRouter, *mockDB) {
	t.Helper()
	db := newMockDB()
	r, err := NewPassthroughRouter(db.reader(), db.writer(), nil)
	require.NoError(t, err)
	return r, db
}

// TestPassthroughRouterRequiresReader verifies that NewPassthroughRouter
// rejects a nil reader. The router has no internal default and would
// nil-panic on the first Read call if we let it through.
func TestPassthroughRouterRequiresReader(t *testing.T) {
	db := newMockDB()
	r, err := NewPassthroughRouter(nil, db.writer(), nil)
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "reader")
}

// TestPassthroughRouterRequiresWriter verifies that NewPassthroughRouter
// rejects a nil writer. ApplyChangeSets has no fallback path.
func TestPassthroughRouterRequiresWriter(t *testing.T) {
	db := newMockDB()
	r, err := NewPassthroughRouter(db.reader(), nil, nil)
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "writer")
}

// TestPassthroughRouterReadForwardsAnyName is the core property test:
// the passthrough router never inspects the store name. Reads for
// names that are not in keys.MemIAVLStoreKeys (e.g. icahost) must
// still hit the backend.
func TestPassthroughRouterReadForwardsAnyName(t *testing.T) {
	r, db := newPassthroughRouterForTest(t)
	db.seed(map[string]map[string][]byte{
		"icahost": {"k": []byte("v")},
	})

	got, ok, err := r.Read("icahost", []byte("k"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("v"), got)
}

// TestPassthroughRouterReadPropagatesReaderError verifies that a
// reader error is returned verbatim rather than masked by a routing
// error. ModuleRouter would have rejected unknown names before
// calling the reader; the passthrough router must not.
func TestPassthroughRouterReadPropagatesReaderError(t *testing.T) {
	sentinel := errors.New("backend exploded")
	r, err := NewPassthroughRouter(failReader(sentinel), newMockDB().writer(), nil)
	require.NoError(t, err)

	_, _, err = r.Read("anything", []byte("k"))
	require.ErrorIs(t, err, sentinel)
}

// TestPassthroughRouterApplyChangeSetsForwardsAnyName verifies that
// writes to names outside keys.MemIAVLStoreKeys are accepted and
// persisted. The mockDB writer records the raw batch so we can
// confirm the changesets reach it unmodified.
func TestPassthroughRouterApplyChangeSetsForwardsAnyName(t *testing.T) {
	r, db := newPassthroughRouterForTest(t)

	batch := []*proto.NamedChangeSet{
		{Name: "icahost", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k1"), Value: []byte("v1")},
		}}},
		{Name: "icacontroller", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k2"), Value: []byte("v2")},
		}}},
	}
	require.NoError(t, r.ApplyChangeSets(batch, true))

	require.Len(t, db.writeLog, 1)
	require.Equal(t, batch, db.writeLog[0])

	v, ok := db.get("icahost", "k1")
	require.True(t, ok)
	require.Equal(t, []byte("v1"), v)
	v, ok = db.get("icacontroller", "k2")
	require.True(t, ok)
	require.Equal(t, []byte("v2"), v)
}

// TestPassthroughRouterApplyChangeSetsPropagatesWriterError verifies
// that the writer's error surfaces unwrapped.
func TestPassthroughRouterApplyChangeSetsPropagatesWriterError(t *testing.T) {
	sentinel := errors.New("backend exploded")
	r, err := NewPassthroughRouter(newMockDB().reader(), failWriter(sentinel), nil)
	require.NoError(t, err)

	err = r.ApplyChangeSets([]*proto.NamedChangeSet{{Name: "anything"}}, true)
	require.ErrorIs(t, err, sentinel)
}

// TestPassthroughRouterGetProofWithoutBuilder verifies that a router
// constructed without a proof builder yields a clear error.
func TestPassthroughRouterGetProofWithoutBuilder(t *testing.T) {
	r, _ := newPassthroughRouterForTest(t)

	p, err := r.GetProof("icahost", []byte("k"))
	require.Error(t, err)
	require.Nil(t, p)
	require.Contains(t, err.Error(), "proofs not supported")
	require.Contains(t, err.Error(), "icahost")
}

// TestPassthroughRouterGetProofForwardsToBuilder verifies that when a
// proof builder is supplied, the call forwards with arguments intact
// and the builder's output is returned verbatim.
func TestPassthroughRouterGetProofForwardsToBuilder(t *testing.T) {
	want := &ics23.CommitmentProof{}
	var captured struct {
		store  string
		key    []byte
		called bool
	}
	builder := func(store string, key []byte) (*ics23.CommitmentProof, error) {
		captured.store = store
		captured.key = key
		captured.called = true
		return want, nil
	}
	r, err := NewPassthroughRouter(newMockDB().reader(), newMockDB().writer(), builder)
	require.NoError(t, err)

	got, err := r.GetProof("icahost", []byte("k"))
	require.NoError(t, err)
	require.True(t, captured.called)
	require.Equal(t, "icahost", captured.store)
	require.Equal(t, []byte("k"), captured.key)
	require.Same(t, want, got)
}
