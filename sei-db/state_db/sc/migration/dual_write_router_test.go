package migration

import (
	"errors"
	"testing"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// noopProof returns (nil, nil) for any input. Used to satisfy the
// constructor's strict non-nil proof-builder requirement when a test
// does not exercise proofs.
func noopProof(_ string, _ []byte) (*ics23.CommitmentProof, error) {
	return nil, nil
}

// --- Constructor tests ---

func TestNewTestOnlyDualWriteRouter_Success(t *testing.T) {
	primary := newRouteWithBuilders(t, newMockDB(), noopProof, "evm")
	r, err := NewTestOnlyDualWriteRouter(primary, newMockDB().writer())
	require.NoError(t, err)
	require.NotNil(t, r)
}

func TestNewTestOnlyDualWriteRouter_NilPrimaryRejected(t *testing.T) {
	r, err := NewTestOnlyDualWriteRouter(nil, newMockDB().writer())
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "primary")
}

func TestNewTestOnlyDualWriteRouter_NilSecondaryRejected(t *testing.T) {
	primary := newRouteWithBuilders(t, newMockDB(), noopProof, "evm")
	r, err := NewTestOnlyDualWriteRouter(primary, nil)
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "secondary")
}

// TestNewTestOnlyDualWriteRouter_NilProofBuilderRejected pins the
// strict-at-construction contract: a primary with a nil proofBuilder is
// rejected. This is intentionally stricter than ModuleRouter (which
// errors lazily at GetProof time) and means a route built by
// routeToFlatKV (which deliberately passes a nil proof builder) cannot
// be the primary today.
func TestNewTestOnlyDualWriteRouter_NilProofBuilderRejected(t *testing.T) {
	primary := newRouteWithBuilders(t, newMockDB(), nil, "evm")
	r, err := NewTestOnlyDualWriteRouter(primary, newMockDB().writer())
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "proof builder")
}

// --- ApplyChangeSets tests ---
//
// Ordering and short-circuit behavior are NOT pinned by these tests:
// callers are expected to restore both backends to a safe snapshot on
// any error, so the dual-write router makes no guarantees about which
// writer runs first or whether one runs when the other fails.

func TestApplyChangeSets_FansOutToBoth(t *testing.T) {
	primaryDB := newMockDB()
	secondaryDB := newMockDB()
	primary := newRouteWithBuilders(t, primaryDB, noopProof, "evm")
	r, err := NewTestOnlyDualWriteRouter(primary, secondaryDB.writer())
	require.NoError(t, err)

	require.NoError(t, r.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("evm", kv("k", "v")),
	}, true))

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
		noopProof,
		"evm",
	)
	require.NoError(t, err)
	r, err := NewTestOnlyDualWriteRouter(primary, newMockDB().writer())
	require.NoError(t, err)

	applyErr := r.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("evm", kv("k", "v")),
	}, true)
	require.Error(t, applyErr)
	require.ErrorIs(t, applyErr, sentinel)
	require.Contains(t, applyErr.Error(), "primary writer")
}

func TestApplyChangeSets_SecondaryErrorPropagated(t *testing.T) {
	sentinel := errors.New("secondary boom")
	primary := newRouteWithBuilders(t, newMockDB(), noopProof, "evm")
	r, err := NewTestOnlyDualWriteRouter(primary, failWriter(sentinel))
	require.NoError(t, err)

	applyErr := r.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("evm", kv("k", "v")),
	}, true)
	require.Error(t, applyErr)
	require.ErrorIs(t, applyErr, sentinel)
	require.Contains(t, applyErr.Error(), "secondary writer")
}

// TestApplyChangeSets_ForwardsAllChangesetsRegardlessOfPrimaryModules
// pins the deliberate "no module-filtering" behavior: the dual-write
// router hands every changeset to both writers regardless of what
// modules the primary route claims to handle. The composition story
// relies on an outer ModuleRouter doing the gating.
func TestApplyChangeSets_ForwardsAllChangesetsRegardlessOfPrimaryModules(t *testing.T) {
	primaryDB := newMockDB()
	secondaryDB := newMockDB()
	primary := newRouteWithBuilders(t, primaryDB, noopProof, "evm")
	r, err := NewTestOnlyDualWriteRouter(primary, secondaryDB.writer())
	require.NoError(t, err)

	require.NoError(t, r.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("evm", kv("ek", "ev")),
		namedCS("bank", kv("bk", "bv")),
	}, true))

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
		func(_ []*proto.NamedChangeSet, _ bool) error {
			primaryCalls++
			return nil
		},
		noopProof,
		"evm",
	)
	require.NoError(t, err)

	secondaryCalls := 0
	secondary := func(_ []*proto.NamedChangeSet, _ bool) error {
		secondaryCalls++
		return nil
	}

	r, err := NewTestOnlyDualWriteRouter(primary, secondary)
	require.NoError(t, err)

	require.NoError(t, r.ApplyChangeSets(nil, true))
	require.NoError(t, r.ApplyChangeSets([]*proto.NamedChangeSet{}, true))

	require.Equal(t, 2, primaryCalls)
	require.Equal(t, 2, secondaryCalls)
}

// --- Read tests ---

func TestRead_DelegatesToPrimary(t *testing.T) {
	primaryDB := newMockDB()
	primaryDB.seed(map[string]map[string][]byte{"evm": {"k": []byte("v")}})
	primary := newRouteWithBuilders(t, primaryDB, noopProof, "evm")
	r, err := NewTestOnlyDualWriteRouter(primary, newMockDB().writer())
	require.NoError(t, err)

	val, found, err := r.Read("evm", []byte("k"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v"), val)
}

func TestRead_NotFoundReturnsFalse(t *testing.T) {
	primary := newRouteWithBuilders(t, newMockDB(), noopProof, "evm")
	r, err := NewTestOnlyDualWriteRouter(primary, newMockDB().writer())
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
		noopProof,
		"evm",
	)
	require.NoError(t, err)
	r, err := NewTestOnlyDualWriteRouter(primary, newMockDB().writer())
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
	primary := newRouteWithBuilders(t, primaryDB, noopProof, "evm")

	secondaryCalls := 0
	secondary := func(_ []*proto.NamedChangeSet, _ bool) error {
		secondaryCalls++
		return nil
	}
	r, err := NewTestOnlyDualWriteRouter(primary, secondary)
	require.NoError(t, err)

	_, _, err = r.Read("evm", []byte("k"))
	require.NoError(t, err)
	_, _, err = r.Read("evm", []byte("missing"))
	require.NoError(t, err)
	require.Equal(t, 0, secondaryCalls, "Read must never invoke the secondary writer")
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
	primary := newRouteWithBuilders(t, newMockDB(), recordingProof, "evm")
	r, err := NewTestOnlyDualWriteRouter(primary, newMockDB().writer())
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
	primary := newRouteWithBuilders(t, newMockDB(), failingProof, "evm")
	r, err := NewTestOnlyDualWriteRouter(primary, newMockDB().writer())
	require.NoError(t, err)

	proof, proofErr := r.GetProof("evm", []byte("k"))
	require.Error(t, proofErr)
	require.ErrorIs(t, proofErr, sentinel)
	require.Contains(t, proofErr.Error(), "primary proof builder")
	require.Nil(t, proof)
}

// --- BuildRoute tests ---
//
// BuildRoute returns a *Route whose function fields are method values
// bound to the dual-write router. These tests exercise the route in the
// same way ModuleRouter would: by invoking the route's function fields
// directly. Ordering and short-circuit behavior on writer errors are
// intentionally NOT pinned, matching the convention established above
// for ApplyChangeSets.

func TestDualWriteBuildRoute_ReturnsValidRoute(t *testing.T) {
	primary := newRouteWithBuilders(t, newMockDB(), noopProof, "evm")
	dwr, err := NewTestOnlyDualWriteRouter(primary, newMockDB().writer())
	require.NoError(t, err)

	route, err := dwr.BuildRoute("evm", "bank")
	require.NoError(t, err)
	require.NotNil(t, route)
	require.Equal(t, []string{"evm", "bank"}, route.modules)
	require.NotNil(t, route.reader)
	require.NotNil(t, route.writer)
	require.NotNil(t, route.proofBuilder)
}

func TestDualWriteBuildRoute_DuplicateModuleNamesRejected(t *testing.T) {
	primary := newRouteWithBuilders(t, newMockDB(), noopProof, "evm")
	dwr, err := NewTestOnlyDualWriteRouter(primary, newMockDB().writer())
	require.NoError(t, err)

	route, err := dwr.BuildRoute("evm", "bank", "evm")
	require.Error(t, err)
	require.Nil(t, route)
	require.Contains(t, err.Error(), "evm")
	require.Contains(t, err.Error(), "more than once")
}

func TestDualWriteBuildRoute_EmptyModulesAllowed(t *testing.T) {
	primary := newRouteWithBuilders(t, newMockDB(), noopProof, "evm")
	dwr, err := NewTestOnlyDualWriteRouter(primary, newMockDB().writer())
	require.NoError(t, err)

	route, err := dwr.BuildRoute()
	require.NoError(t, err)
	require.NotNil(t, route)
}

func TestDualWriteBuildRoute_ReaderDispatchesToPrimary(t *testing.T) {
	primaryDB := newMockDB()
	primaryDB.seed(map[string]map[string][]byte{"evm": {"k": []byte("v")}})
	primary := newRouteWithBuilders(t, primaryDB, noopProof, "evm")

	secondaryCalls := 0
	secondary := func(_ []*proto.NamedChangeSet, _ bool) error {
		secondaryCalls++
		return nil
	}
	dwr, err := NewTestOnlyDualWriteRouter(primary, secondary)
	require.NoError(t, err)
	route, err := dwr.BuildRoute("evm")
	require.NoError(t, err)

	val, found, err := route.reader("evm", []byte("k"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v"), val)
	require.Equal(t, 0, secondaryCalls,
		"secondary writer must not be consulted on reads through the route")
}

func TestDualWriteBuildRoute_ReaderErrorsWrapped(t *testing.T) {
	sentinel := errors.New("disk on fire")
	primary, err := NewRoute(failReader(sentinel),
		newMockDB().writer(),
		noopProof,
		"evm")
	require.NoError(t, err)
	dwr, err := NewTestOnlyDualWriteRouter(primary, newMockDB().writer())
	require.NoError(t, err)
	route, err := dwr.BuildRoute("evm")
	require.NoError(t, err)

	val, found, readErr := route.reader("evm", []byte("k"))
	require.Error(t, readErr)
	require.ErrorIs(t, readErr, sentinel)
	require.Contains(t, readErr.Error(), "primary reader")
	require.False(t, found)
	require.Nil(t, val)
}

func TestDualWriteBuildRoute_WriterFansOutToBothBackends(t *testing.T) {
	primaryDB := newMockDB()
	secondaryDB := newMockDB()
	primary := newRouteWithBuilders(t, primaryDB, noopProof, "evm")
	dwr, err := NewTestOnlyDualWriteRouter(primary, secondaryDB.writer())
	require.NoError(t, err)
	route, err := dwr.BuildRoute("evm")
	require.NoError(t, err)

	require.NoError(t, route.writer([]*proto.NamedChangeSet{
		namedCS("evm", kv("k", "v")),
	}, true))

	pv, ok := primaryDB.get("evm", "k")
	require.True(t, ok, "primary must receive the write through the route")
	require.Equal(t, []byte("v"), pv)
	sv, ok := secondaryDB.get("evm", "k")
	require.True(t, ok, "secondary must receive the write through the route")
	require.Equal(t, []byte("v"), sv)
}

func TestDualWriteBuildRoute_WriterPrimaryErrorWrapped(t *testing.T) {
	sentinel := errors.New("primary boom")
	primary, err := NewRoute(newMockDB().reader(),
		failWriter(sentinel),
		noopProof,
		"evm")
	require.NoError(t, err)
	dwr, err := NewTestOnlyDualWriteRouter(primary, newMockDB().writer())
	require.NoError(t, err)
	route, err := dwr.BuildRoute("evm")
	require.NoError(t, err)

	writeErr := route.writer([]*proto.NamedChangeSet{namedCS("evm", kv("k", "v"))}, true)
	require.Error(t, writeErr)
	require.ErrorIs(t, writeErr, sentinel)
	require.Contains(t, writeErr.Error(), "primary writer")
}

func TestDualWriteBuildRoute_WriterSecondaryErrorWrapped(t *testing.T) {
	sentinel := errors.New("secondary boom")
	primary := newRouteWithBuilders(t, newMockDB(), noopProof, "evm")
	dwr, err := NewTestOnlyDualWriteRouter(primary, failWriter(sentinel))
	require.NoError(t, err)
	route, err := dwr.BuildRoute("evm")
	require.NoError(t, err)

	writeErr := route.writer([]*proto.NamedChangeSet{namedCS("evm", kv("k", "v"))}, true)
	require.Error(t, writeErr)
	require.ErrorIs(t, writeErr, sentinel)
	require.Contains(t, writeErr.Error(), "secondary writer")
}

func TestDualWriteBuildRoute_ProofDelegatesToPrimary(t *testing.T) {
	wantProof := &ics23.CommitmentProof{}
	recordingProof, calls := recordingProofBuilder(wantProof, nil)
	primary := newRouteWithBuilders(t, newMockDB(), recordingProof, "evm")
	dwr, err := NewTestOnlyDualWriteRouter(primary, newMockDB().writer())
	require.NoError(t, err)
	route, err := dwr.BuildRoute("evm")
	require.NoError(t, err)

	gotProof, err := route.proofBuilder("evm", []byte("k"))
	require.NoError(t, err)
	require.Same(t, wantProof, gotProof,
		"route proof builder must return the exact proof from the primary's builder")
	require.Len(t, *calls, 1)
	require.Equal(t, "evm", (*calls)[0].store)
	require.Equal(t, []byte("k"), (*calls)[0].key)
}

func TestDualWriteBuildRoute_ProofErrorsWrapped(t *testing.T) {
	sentinel := errors.New("proof boom")
	failingProof, _ := recordingProofBuilder(nil, sentinel)
	primary := newRouteWithBuilders(t, newMockDB(), failingProof, "evm")
	dwr, err := NewTestOnlyDualWriteRouter(primary, newMockDB().writer())
	require.NoError(t, err)
	route, err := dwr.BuildRoute("evm")
	require.NoError(t, err)

	proof, proofErr := route.proofBuilder("evm", []byte("k"))
	require.Error(t, proofErr)
	require.ErrorIs(t, proofErr, sentinel)
	require.Contains(t, proofErr.Error(), "primary proof builder")
	require.Nil(t, proof)
}

// TestDualWriteBuildRoute_IntegrationWithModuleRouter exercises the
// composition story BuildRoute is designed for: the dual-write router
// contributes a Route for one module ("evm"), and an unrelated route
// owns another ("bank"). Through the outer ModuleRouter, evm writes
// must fan out to both primary and secondary, while non-evm writes
// must never reach the secondary.
func TestDualWriteBuildRoute_IntegrationWithModuleRouter(t *testing.T) {
	primaryDB := newMockDB()
	secondaryDB := newMockDB()
	primary := newRouteWithBuilders(t, primaryDB, noopProof, "evm")
	dwr, err := NewTestOnlyDualWriteRouter(primary, secondaryDB.writer())
	require.NoError(t, err)

	dualWriteRoute, err := dwr.BuildRoute("evm")
	require.NoError(t, err)

	otherDB := newMockDB()
	otherRoute := newRouteWithBuilders(t, otherDB, noopProof, "bank")

	router, err := NewModuleRouter(dualWriteRoute, otherRoute)
	require.NoError(t, err)

	require.NoError(t, router.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("evm", kv("k1", "v1")),
		namedCS("bank", kv("k2", "v2")),
	}, true))

	v, ok := primaryDB.get("evm", "k1")
	require.True(t, ok, "evm primary must receive evm writes")
	require.Equal(t, []byte("v1"), v)
	v, ok = secondaryDB.get("evm", "k1")
	require.True(t, ok, "evm secondary must receive evm writes")
	require.Equal(t, []byte("v1"), v)

	_, ok = primaryDB.get("bank", "k2")
	require.False(t, ok, "bank writes must not leak into evm primary")
	_, ok = secondaryDB.get("bank", "k2")
	require.False(t, ok, "bank writes must not leak into evm secondary")

	v, ok = otherDB.get("bank", "k2")
	require.True(t, ok, "bank backing must receive bank writes")
	require.Equal(t, []byte("v2"), v)
	_, ok = otherDB.get("evm", "k1")
	require.False(t, ok, "evm writes must not leak into the bank backing")

	val, found, err := router.Read("evm", []byte("k1"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v1"), val,
		"reads for evm through the outer router come from the primary only")

	val, found, err = router.Read("bank", []byte("k2"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v2"), val)
}
