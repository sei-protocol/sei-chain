package migration

import (
	"errors"
	"testing"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

// noopIter returns (nil, nil) for any input. Used to satisfy the
// constructor's strict non-nil iterator-builder requirement when a test
// does not exercise iteration.
func noopIter(_ string, _, _ []byte, _ bool) (dbm.Iterator, error) {
	return nil, nil
}

// noopProof returns (nil, nil) for any input. Used to satisfy the
// constructor's strict non-nil proof-builder requirement when a test
// does not exercise proofs.
func noopProof(_ string, _ []byte) (*ics23.CommitmentProof, error) {
	return nil, nil
}

// --- Constructor tests ---

func TestNewTestOnlyDuplicatingRouter_Success(t *testing.T) {
	primary := newRouteWithBuilders(t, newMockDB(), noopIter, noopProof, "evm")
	r, err := NewTestOnlyDuplicatingRouter(primary, newMockDB().writer())
	require.NoError(t, err)
	require.NotNil(t, r)
}

func TestNewTestOnlyDuplicatingRouter_NilPrimaryRejected(t *testing.T) {
	r, err := NewTestOnlyDuplicatingRouter(nil, newMockDB().writer())
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "primary")
}

func TestNewTestOnlyDuplicatingRouter_NilSecondaryRejected(t *testing.T) {
	primary := newRouteWithBuilders(t, newMockDB(), noopIter, noopProof, "evm")
	r, err := NewTestOnlyDuplicatingRouter(primary, nil)
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "secondary")
}

// TestNewTestOnlyDuplicatingRouter_NilProofBuilderRejected pins the
// strict-at-construction contract: a primary with a nil proofBuilder is
// rejected. This is intentionally stricter than ModuleRouter (which
// errors lazily at GetProof time) and means a route built by
// routeToFlatKV (which deliberately passes nil for both builders) cannot
// be the primary today.
func TestNewTestOnlyDuplicatingRouter_NilProofBuilderRejected(t *testing.T) {
	primary := newRouteWithBuilders(t, newMockDB(), noopIter, nil, "evm")
	r, err := NewTestOnlyDuplicatingRouter(primary, newMockDB().writer())
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "proof builder")
}

// TestNewTestOnlyDuplicatingRouter_NilIteratorBuilderRejected pins the
// strict-at-construction contract: a primary with a nil iteratorBuilder
// is rejected. Same rationale as the proofBuilder case above.
func TestNewTestOnlyDuplicatingRouter_NilIteratorBuilderRejected(t *testing.T) {
	primary := newRouteWithBuilders(t, newMockDB(), nil, noopProof, "evm")
	r, err := NewTestOnlyDuplicatingRouter(primary, newMockDB().writer())
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "iterator builder")
}

// --- ApplyChangeSets tests ---
//
// Ordering and short-circuit behavior are NOT pinned by these tests:
// callers are expected to restore both backends to a safe snapshot on
// any error, so the duplicator makes no guarantees about which writer
// runs first or whether one runs when the other fails.

func TestApplyChangeSets_FansOutToBoth(t *testing.T) {
	primaryDB := newMockDB()
	secondaryDB := newMockDB()
	primary := newRouteWithBuilders(t, primaryDB, noopIter, noopProof, "evm")
	r, err := NewTestOnlyDuplicatingRouter(primary, secondaryDB.writer())
	require.NoError(t, err)

	require.NoError(t, r.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("evm", kv("k", "v")),
	}))

	pv, ok := primaryDB.get("evm", "k")
	require.True(t, ok, "primary must receive the write")
	require.Equal(t, []byte("v"), pv)

	sv, ok := secondaryDB.get("evm", "k")
	require.True(t, ok, "secondary must receive the write")
	require.Equal(t, []byte("v"), sv)
}

func TestApplyChangeSets_PrimaryErrorPropagated(t *testing.T) {
	sentinel := errors.New("primary boom")
	primary, err := NewRoute(
		newMockDB().reader(),
		failWriter(sentinel),
		noopIter, noopProof,
		"evm",
	)
	require.NoError(t, err)
	r, err := NewTestOnlyDuplicatingRouter(primary, newMockDB().writer())
	require.NoError(t, err)

	applyErr := r.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("evm", kv("k", "v")),
	})
	require.Error(t, applyErr)
	require.ErrorIs(t, applyErr, sentinel)
	require.Contains(t, applyErr.Error(), "primary writer")
}

func TestApplyChangeSets_SecondaryErrorPropagated(t *testing.T) {
	sentinel := errors.New("secondary boom")
	primary := newRouteWithBuilders(t, newMockDB(), noopIter, noopProof, "evm")
	r, err := NewTestOnlyDuplicatingRouter(primary, failWriter(sentinel))
	require.NoError(t, err)

	applyErr := r.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("evm", kv("k", "v")),
	})
	require.Error(t, applyErr)
	require.ErrorIs(t, applyErr, sentinel)
	require.Contains(t, applyErr.Error(), "secondary writer")
}

// TestApplyChangeSets_ForwardsAllChangesetsRegardlessOfPrimaryModules
// pins the deliberate "no module-filtering" behavior: the duplicator
// hands every changeset to both writers regardless of what modules the
// primary route claims to handle. The composition story relies on an
// outer ModuleRouter doing the gating.
func TestApplyChangeSets_ForwardsAllChangesetsRegardlessOfPrimaryModules(t *testing.T) {
	primaryDB := newMockDB()
	secondaryDB := newMockDB()
	primary := newRouteWithBuilders(t, primaryDB, noopIter, noopProof, "evm")
	r, err := NewTestOnlyDuplicatingRouter(primary, secondaryDB.writer())
	require.NoError(t, err)

	require.NoError(t, r.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("evm", kv("ek", "ev")),
		namedCS("bank", kv("bk", "bv")),
	}))

	for _, db := range []*mockDB{primaryDB, secondaryDB} {
		v, ok := db.get("evm", "ek")
		require.True(t, ok, "evm changeset must reach both writers")
		require.Equal(t, []byte("ev"), v)
		v, ok = db.get("bank", "bk")
		require.True(t, ok, "bank changeset must reach both writers despite primary listing only evm")
		require.Equal(t, []byte("bv"), v)
	}
}

func TestApplyChangeSets_NilAndEmptyChangesetsForwarded(t *testing.T) {
	primaryCalls := 0
	primary, err := NewRoute(
		newMockDB().reader(),
		func(_ []*proto.NamedChangeSet) error {
			primaryCalls++
			return nil
		},
		noopIter, noopProof,
		"evm",
	)
	require.NoError(t, err)

	secondaryCalls := 0
	secondary := func(_ []*proto.NamedChangeSet) error {
		secondaryCalls++
		return nil
	}

	r, err := NewTestOnlyDuplicatingRouter(primary, secondary)
	require.NoError(t, err)

	require.NoError(t, r.ApplyChangeSets(nil))
	require.NoError(t, r.ApplyChangeSets([]*proto.NamedChangeSet{}))

	require.Equal(t, 2, primaryCalls)
	require.Equal(t, 2, secondaryCalls)
}

// --- Read tests ---

func TestRead_DelegatesToPrimary(t *testing.T) {
	primaryDB := newMockDB()
	primaryDB.seed(map[string]map[string][]byte{"evm": {"k": []byte("v")}})
	primary := newRouteWithBuilders(t, primaryDB, noopIter, noopProof, "evm")
	r, err := NewTestOnlyDuplicatingRouter(primary, newMockDB().writer())
	require.NoError(t, err)

	val, found, err := r.Read("evm", []byte("k"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v"), val)
}

func TestRead_NotFoundReturnsFalse(t *testing.T) {
	primary := newRouteWithBuilders(t, newMockDB(), noopIter, noopProof, "evm")
	r, err := NewTestOnlyDuplicatingRouter(primary, newMockDB().writer())
	require.NoError(t, err)

	val, found, err := r.Read("evm", []byte("missing"))
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, val)
}

func TestRead_PrimaryErrorWrapped(t *testing.T) {
	sentinel := errors.New("disk on fire")
	primary, err := NewRoute(
		failReader(sentinel),
		newMockDB().writer(),
		noopIter, noopProof,
		"evm",
	)
	require.NoError(t, err)
	r, err := NewTestOnlyDuplicatingRouter(primary, newMockDB().writer())
	require.NoError(t, err)

	val, found, readErr := r.Read("evm", []byte("k"))
	require.Error(t, readErr)
	require.ErrorIs(t, readErr, sentinel)
	require.Contains(t, readErr.Error(), "primary reader")
	require.False(t, found)
	require.Nil(t, val)
}

func TestRead_DoesNotConsultSecondary(t *testing.T) {
	primaryDB := newMockDB()
	primaryDB.seed(map[string]map[string][]byte{"evm": {"k": []byte("v")}})
	primary := newRouteWithBuilders(t, primaryDB, noopIter, noopProof, "evm")

	secondaryCalls := 0
	secondary := func(_ []*proto.NamedChangeSet) error {
		secondaryCalls++
		return nil
	}
	r, err := NewTestOnlyDuplicatingRouter(primary, secondary)
	require.NoError(t, err)

	_, _, err = r.Read("evm", []byte("k"))
	require.NoError(t, err)
	_, _, err = r.Read("evm", []byte("missing"))
	require.NoError(t, err)
	require.Equal(t, 0, secondaryCalls, "Read must never invoke the secondary writer")
}

// --- Iterator tests ---

func TestIterator_DelegatesToPrimary(t *testing.T) {
	var (
		gotStore     string
		gotStart     []byte
		gotEnd       []byte
		gotAscending bool
	)
	recordingIter := func(store string, start []byte, end []byte, ascending bool) (dbm.Iterator, error) {
		gotStore = store
		gotStart = start
		gotEnd = end
		gotAscending = ascending
		return nil, nil
	}
	primary := newRouteWithBuilders(t, newMockDB(), recordingIter, noopProof, "evm")
	r, err := NewTestOnlyDuplicatingRouter(primary, newMockDB().writer())
	require.NoError(t, err)

	iter, err := r.Iterator("evm", []byte("a"), []byte("z"), true)
	require.NoError(t, err)
	require.Nil(t, iter)
	require.Equal(t, "evm", gotStore)
	require.Equal(t, []byte("a"), gotStart)
	require.Equal(t, []byte("z"), gotEnd)
	require.True(t, gotAscending)
}

func TestIterator_PrimaryErrorWrapped(t *testing.T) {
	sentinel := errors.New("iterator boom")
	failingIter := func(_ string, _, _ []byte, _ bool) (dbm.Iterator, error) {
		return nil, sentinel
	}
	primary := newRouteWithBuilders(t, newMockDB(), failingIter, noopProof, "evm")
	r, err := NewTestOnlyDuplicatingRouter(primary, newMockDB().writer())
	require.NoError(t, err)

	iter, iterErr := r.Iterator("evm", []byte("a"), []byte("z"), true)
	require.Error(t, iterErr)
	require.ErrorIs(t, iterErr, sentinel)
	require.Contains(t, iterErr.Error(), "primary iterator builder")
	require.Nil(t, iter)
}

// --- GetProof tests ---

func TestGetProof_DelegatesToPrimary(t *testing.T) {
	var (
		gotStore string
		gotKey   []byte
	)
	sentinelProof := &ics23.CommitmentProof{}
	recordingProof := func(store string, key []byte) (*ics23.CommitmentProof, error) {
		gotStore = store
		gotKey = key
		return sentinelProof, nil
	}
	primary := newRouteWithBuilders(t, newMockDB(), noopIter, recordingProof, "evm")
	r, err := NewTestOnlyDuplicatingRouter(primary, newMockDB().writer())
	require.NoError(t, err)

	proof, err := r.GetProof("evm", []byte("k"))
	require.NoError(t, err)
	require.Same(t, sentinelProof, proof)
	require.Equal(t, "evm", gotStore)
	require.Equal(t, []byte("k"), gotKey)
}

func TestGetProof_PrimaryErrorWrapped(t *testing.T) {
	sentinel := errors.New("proof boom")
	failingProof := func(_ string, _ []byte) (*ics23.CommitmentProof, error) {
		return nil, sentinel
	}
	primary := newRouteWithBuilders(t, newMockDB(), noopIter, failingProof, "evm")
	r, err := NewTestOnlyDuplicatingRouter(primary, newMockDB().writer())
	require.NoError(t, err)

	proof, proofErr := r.GetProof("evm", []byte("k"))
	require.Error(t, proofErr)
	require.ErrorIs(t, proofErr, sentinel)
	require.Contains(t, proofErr.Error(), "primary proof builder")
	require.Nil(t, proof)
}
