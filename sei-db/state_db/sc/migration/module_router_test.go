package migration

import (
	"errors"
	"fmt"
	"testing"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// newRoute wires a mockDB into a Route for the given modules. The proof
// builder is nil; tests that need one should call NewRoute directly or
// use newRouteWithBuilders.
func newRoute(t *testing.T, db *mockDB, modules ...string) *Route {
	t.Helper()
	r, err := NewRoute(db.reader(), db.writer(), nil, modules...)
	require.NoError(t, err)
	return r
}

// newRouteWithBuilders wires a mockDB into a Route with an explicit
// proof builder (which may be nil).
func newRouteWithBuilders(
	t *testing.T,
	db *mockDB,
	proof DBProofBuilder,
	modules ...string,
) *Route {
	t.Helper()
	r, err := NewRoute(db.reader(), db.writer(), proof, modules...)
	require.NoError(t, err)
	return r
}

// newTestRouter wires up a ModuleRouter backed by two fresh mockDBs and
// returns the router along with the backing DBs so tests can seed state
// and assert on persisted writes.
func newTestRouter(t *testing.T, aModules, bModules []string) (*ModuleRouter, *mockDB, *mockDB) {
	t.Helper()
	dbA := newMockDB()
	dbB := newMockDB()
	r, err := NewModuleRouter(
		newRoute(t, dbA, aModules...),
		newRoute(t, dbB, bModules...),
	)
	require.NoError(t, err)
	return r, dbA, dbB
}

// --- Route constructor tests ---

func TestNewRoute_NilArgumentsRejected(t *testing.T) {
	db := newMockDB()

	tests := []struct {
		name   string
		reader DBReader
		writer DBWriter
		errSub string
	}{
		{"nil reader", nil, db.writer(), "reader"},
		{"nil writer", db.reader(), nil, "writer"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := NewRoute(tc.reader, tc.writer, nil, "evm")
			require.Error(t, err)
			require.Nil(t, r)
			require.Contains(t, err.Error(), tc.errSub)
		})
	}
}

func TestNewRoute_NilBuildersAllowed(t *testing.T) {
	// The proof builder is optional; nil is the documented way to
	// indicate the route does not support proofs.
	db := newMockDB()
	r, err := NewRoute(db.reader(), db.writer(), nil, "evm")
	require.NoError(t, err)
	require.NotNil(t, r)
}

func TestNewRoute_BuildersStoredOnRoute(t *testing.T) {
	db := newMockDB()
	proof := func(_ string, _ []byte) (*ics23.CommitmentProof, error) { return nil, nil }

	r, err := NewRoute(db.reader(), db.writer(), proof, "evm")
	require.NoError(t, err)
	require.NotNil(t, r)
	require.NotNil(t, r.proofBuilder)
}

func TestNewRoute_EmptyModulesAllowed(t *testing.T) {
	db := newMockDB()
	r, err := NewRoute(db.reader(), db.writer(), nil)
	require.NoError(t, err)
	require.NotNil(t, r)
}

func TestNewRoute_DuplicateModulesRejected(t *testing.T) {
	db := newMockDB()
	r, err := NewRoute(db.reader(), db.writer(), nil, "evm", "bank", "evm")
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "evm")
	require.Contains(t, err.Error(), "more than once")
}

func TestNewRoute_AdjacentDuplicatesRejected(t *testing.T) {
	// Sanity check that detection doesn't depend on the duplicate being
	// far away in the slice.
	db := newMockDB()
	r, err := NewRoute(db.reader(), db.writer(), nil, "evm", "evm")
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "evm")
}

// --- ModuleRouter constructor tests ---

func TestNewModuleRouter_Success(t *testing.T) {
	dbA := newMockDB()
	dbB := newMockDB()
	r, err := NewModuleRouter(
		newRoute(t, dbA, "evm"),
		newRoute(t, dbB, "bank"),
	)
	require.NoError(t, err)
	require.NotNil(t, r)
}

func TestNewModuleRouter_SingleRouteAllowed(t *testing.T) {
	db := newMockDB()
	r, err := NewModuleRouter(newRoute(t, db, "evm"))
	require.NoError(t, err)
	require.NotNil(t, r)
}

func TestNewModuleRouter_ManyRoutesAllowed(t *testing.T) {
	db1 := newMockDB()
	db2 := newMockDB()
	db3 := newMockDB()
	db4 := newMockDB()
	r, err := NewModuleRouter(
		newRoute(t, db1, "evm"),
		newRoute(t, db2, "bank"),
		newRoute(t, db3, "staking"),
		newRoute(t, db4, "wasm"),
	)
	require.NoError(t, err)
	require.NotNil(t, r)
}

func TestNewModuleRouter_NoRoutesRejected(t *testing.T) {
	r, err := NewModuleRouter()
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "at least one")
}

func TestNewModuleRouter_NilRouteRejected(t *testing.T) {
	dbA := newMockDB()
	r, err := NewModuleRouter(
		newRoute(t, dbA, "evm"),
		nil,
	)
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "must not be nil")
}

func TestNewModuleRouter_OverlappingModulesRejected(t *testing.T) {
	dbA := newMockDB()
	dbB := newMockDB()
	r, err := NewModuleRouter(
		newRoute(t, dbA, "evm", "shared"),
		newRoute(t, dbB, "bank", "shared"),
	)
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "shared")
}

func TestNewModuleRouter_OverlappingModulesAcrossManyRoutesRejected(t *testing.T) {
	db1 := newMockDB()
	db2 := newMockDB()
	db3 := newMockDB()
	r, err := NewModuleRouter(
		newRoute(t, db1, "evm"),
		newRoute(t, db2, "bank"),
		newRoute(t, db3, "evm"), // overlaps with first
	)
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "evm")
}

func TestNewModuleRouter_EmptyModuleSetsAllowed(t *testing.T) {
	// Routes with no modules should be accepted. Any read or write
	// will then error, but construction itself is fine.
	dbA := newMockDB()
	dbB := newMockDB()
	r, err := NewModuleRouter(
		newRoute(t, dbA),
		newRoute(t, dbB),
	)
	require.NoError(t, err)
	require.NotNil(t, r)
}

// --- Read tests ---

func TestRead_RoutesToA(t *testing.T) {
	r, dbA, _ := newTestRouter(t, []string{"evm"}, []string{"bank"})
	dbA.seed(map[string]map[string][]byte{
		"evm": {"k1": []byte("v1")},
	})

	val, ok, err := r.Read("evm", []byte("k1"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("v1"), val)
}

func TestRead_RoutesToB(t *testing.T) {
	r, _, dbB := newTestRouter(t, []string{"evm"}, []string{"bank"})
	dbB.seed(map[string]map[string][]byte{
		"bank": {"k2": []byte("v2")},
	})

	val, ok, err := r.Read("bank", []byte("k2"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("v2"), val)
}

func TestRead_MissingKeyReturnsNotFound(t *testing.T) {
	r, _, _ := newTestRouter(t, []string{"evm"}, []string{"bank"})
	val, ok, err := r.Read("evm", []byte("missing"))
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, val)
}

func TestRead_UnregisteredModuleReturnsError(t *testing.T) {
	r, _, _ := newTestRouter(t, []string{"evm"}, []string{"bank"})
	val, ok, err := r.Read("staking", []byte("k1"))
	require.Error(t, err)
	require.False(t, ok)
	require.Nil(t, val)
	require.Contains(t, err.Error(), "staking")
}

func TestRead_DoesNotFallThroughBetweenDatabases(t *testing.T) {
	// A value with the same key existing in DB B must not be returned
	// when the store is routed to DB A.
	r, _, dbB := newTestRouter(t, []string{"evm"}, []string{"bank"})
	dbB.seed(map[string]map[string][]byte{
		"evm": {"k1": []byte("wrong-db")},
	})

	val, ok, err := r.Read("evm", []byte("k1"))
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, val)
}

func TestRead_ReaderErrorPropagated(t *testing.T) {
	dbB := newMockDB()
	sentinel := errors.New("reader boom")
	rA, err := NewRoute(failReader(sentinel),
		func(_ []*proto.NamedChangeSet, _ bool) error { return nil },
		nil,
		"evm")
	require.NoError(t, err)
	r, err := NewModuleRouter(rA, newRoute(t, dbB, "bank"))
	require.NoError(t, err)

	val, ok, readErr := r.Read("evm", []byte("k1"))
	require.ErrorIs(t, readErr, sentinel)
	require.False(t, ok)
	require.Nil(t, val)
}

// --- ApplyChangeSets tests ---

func namedCS(store string, pairs ...*proto.KVPair) *proto.NamedChangeSet {
	return &proto.NamedChangeSet{
		Name:      store,
		Changeset: proto.ChangeSet{Pairs: pairs},
	}
}

func kv(key, value string) *proto.KVPair {
	return &proto.KVPair{Key: []byte(key), Value: []byte(value)}
}

func del(key string) *proto.KVPair {
	return &proto.KVPair{Key: []byte(key), Delete: true}
}

func TestApplyChangeSets_SplitsBetweenDatabases(t *testing.T) {
	r, dbA, dbB := newTestRouter(t, []string{"evm", "wasm"}, []string{"bank", "staking"})
	err := r.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("evm", kv("ek", "ev")),
		namedCS("bank", kv("bk", "bv")),
		namedCS("wasm", kv("wk", "wv")),
		namedCS("staking", kv("sk", "sv")),
	}, true)
	require.NoError(t, err)

	v, ok := dbA.get("evm", "ek")
	require.True(t, ok)
	require.Equal(t, []byte("ev"), v)
	v, ok = dbA.get("wasm", "wk")
	require.True(t, ok)
	require.Equal(t, []byte("wv"), v)
	_, ok = dbA.get("bank", "bk")
	require.False(t, ok, "bank must not land in dbA")
	_, ok = dbA.get("staking", "sk")
	require.False(t, ok, "staking must not land in dbA")

	v, ok = dbB.get("bank", "bk")
	require.True(t, ok)
	require.Equal(t, []byte("bv"), v)
	v, ok = dbB.get("staking", "sk")
	require.True(t, ok)
	require.Equal(t, []byte("sv"), v)
	_, ok = dbB.get("evm", "ek")
	require.False(t, ok, "evm must not land in dbB")
	_, ok = dbB.get("wasm", "wk")
	require.False(t, ok, "wasm must not land in dbB")
}

func TestApplyChangeSets_SplitsAcrossManyDatabases(t *testing.T) {
	db1 := newMockDB()
	db2 := newMockDB()
	db3 := newMockDB()
	r, err := NewModuleRouter(
		newRoute(t, db1, "evm"),
		newRoute(t, db2, "bank"),
		newRoute(t, db3, "staking"),
	)
	require.NoError(t, err)

	err = r.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("evm", kv("ek", "ev")),
		namedCS("bank", kv("bk", "bv")),
		namedCS("staking", kv("sk", "sv")),
	}, true)
	require.NoError(t, err)

	v, ok := db1.get("evm", "ek")
	require.True(t, ok)
	require.Equal(t, []byte("ev"), v)
	v, ok = db2.get("bank", "bk")
	require.True(t, ok)
	require.Equal(t, []byte("bv"), v)
	v, ok = db3.get("staking", "sk")
	require.True(t, ok)
	require.Equal(t, []byte("sv"), v)
}

// TestModuleRouter_ManyRoutes_ReadAndWriteEndToEnd exercises a router
// with four routes, each owning multiple modules. It verifies that:
//   - writes are routed to the correct route and never leak to others;
//   - every route's writer is invoked exactly once per call (even when
//     no changesets target it);
//   - reads are routed to the same route that owns the module;
//   - reads against an unregistered module return an error.
func TestModuleRouter_ManyRoutes_ReadAndWriteEndToEnd(t *testing.T) {
	db1 := newMockDB()
	db2 := newMockDB()
	db3 := newMockDB()
	db4 := newMockDB()

	// Pre-seed each DB so we can assert reads route correctly without
	// depending on a write happening first.
	db1.seed(map[string]map[string][]byte{"evm": {"seed1": []byte("from-db1")}})
	db2.seed(map[string]map[string][]byte{"bank": {"seed2": []byte("from-db2")}})
	db3.seed(map[string]map[string][]byte{"staking": {"seed3": []byte("from-db3")}})
	db4.seed(map[string]map[string][]byte{"oracle": {"seed4": []byte("from-db4")}})

	r, err := NewModuleRouter(
		newRoute(t, db1, "evm", "wasm"),
		newRoute(t, db2, "bank", "distribution"),
		newRoute(t, db3, "staking", "slashing"),
		newRoute(t, db4, "oracle", "mint"),
	)
	require.NoError(t, err)

	// Write to a subset of the modules across three of the four
	// routes. db4 receives no changesets but its writer must still
	// be invoked once.
	err = r.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("evm", kv("ek", "ev")),
		namedCS("bank", kv("bk", "bv")),
		namedCS("wasm", kv("wk", "wv")),
		namedCS("slashing", kv("sk", "sv")),
		namedCS("distribution", kv("dk", "dv")),
	}, true)
	require.NoError(t, err)

	// Each writer should have been invoked exactly once.
	require.Len(t, db1.writeLog, 1)
	require.Len(t, db2.writeLog, 1)
	require.Len(t, db3.writeLog, 1)
	require.Len(t, db4.writeLog, 1)
	require.Empty(t, db4.writeLog[0], "db4 had no changesets routed to it")

	// Verify writes landed in the correct DB and only that DB.
	expected := []struct {
		db    *mockDB
		store string
		key   string
		value string
	}{
		{db1, "evm", "ek", "ev"},
		{db1, "wasm", "wk", "wv"},
		{db2, "bank", "bk", "bv"},
		{db2, "distribution", "dk", "dv"},
		{db3, "slashing", "sk", "sv"},
	}
	for _, e := range expected {
		v, ok := e.db.get(e.store, e.key)
		require.Truef(t, ok, "expected %s/%s in correct db", e.store, e.key)
		require.Equal(t, []byte(e.value), v)
		// And nowhere else.
		for _, other := range []*mockDB{db1, db2, db3, db4} {
			if other == e.db {
				continue
			}
			_, ok := other.get(e.store, e.key)
			require.Falsef(t, ok, "%s/%s leaked into wrong db", e.store, e.key)
		}
	}

	// Reads should route to the owning route. Mix newly-written and
	// pre-seeded values to confirm both paths.
	readCases := []struct {
		store string
		key   string
		value string
	}{
		{"evm", "ek", "ev"},
		{"wasm", "wk", "wv"},
		{"bank", "bk", "bv"},
		{"distribution", "dk", "dv"},
		{"slashing", "sk", "sv"},
		{"evm", "seed1", "from-db1"},
		{"bank", "seed2", "from-db2"},
		{"staking", "seed3", "from-db3"},
		{"oracle", "seed4", "from-db4"},
	}
	for _, rc := range readCases {
		v, ok, err := r.Read(rc.store, []byte(rc.key))
		require.NoErrorf(t, err, "read %s/%s", rc.store, rc.key)
		require.Truef(t, ok, "read %s/%s missing", rc.store, rc.key)
		require.Equalf(t, []byte(rc.value), v, "read %s/%s value", rc.store, rc.key)
	}

	// Reading a module not registered with any of the four routes
	// must error, even though nine modules are registered in total.
	v, ok, err := r.Read("gov", []byte("k"))
	require.Error(t, err)
	require.False(t, ok)
	require.Nil(t, v)
	require.Contains(t, err.Error(), "gov")
}

func TestApplyChangeSets_DeletePropagatesToCorrectDatabase(t *testing.T) {
	r, dbA, dbB := newTestRouter(t, []string{"evm"}, []string{"bank"})
	dbA.seed(map[string]map[string][]byte{"evm": {"k1": []byte("v1")}})
	dbB.seed(map[string]map[string][]byte{"bank": {"k2": []byte("v2")}})

	err := r.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("evm", del("k1")),
		namedCS("bank", del("k2")),
	}, true)
	require.NoError(t, err)

	_, ok := dbA.get("evm", "k1")
	require.False(t, ok)
	_, ok = dbB.get("bank", "k2")
	require.False(t, ok)
}

func TestApplyChangeSets_EmptyAndNilInputs(t *testing.T) {
	r, dbA, dbB := newTestRouter(t, []string{"evm"}, []string{"bank"})

	require.NoError(t, r.ApplyChangeSets(nil, true))
	require.NoError(t, r.ApplyChangeSets([]*proto.NamedChangeSet{}, true))
	require.NoError(t, r.ApplyChangeSets([]*proto.NamedChangeSet{nil, nil}, true))

	// Each call still invokes both writers once (even with zero work).
	require.Len(t, dbA.writeLog, 3)
	require.Len(t, dbB.writeLog, 3)
	for _, batch := range dbA.writeLog {
		require.Empty(t, batch)
	}
	for _, batch := range dbB.writeLog {
		require.Empty(t, batch)
	}
}

func TestApplyChangeSets_UnregisteredModuleRejectsWholeBatch(t *testing.T) {
	r, dbA, dbB := newTestRouter(t, []string{"evm"}, []string{"bank"})
	err := r.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("evm", kv("ek", "ev")),
		namedCS("staking", kv("sk", "sv")),
		namedCS("bank", kv("bk", "bv")),
	}, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "staking")

	// Validation runs up front, so no writes should have been dispatched.
	require.Empty(t, dbA.writeLog, "no writes should land in dbA when the batch is rejected")
	require.Empty(t, dbB.writeLog, "no writes should land in dbB when the batch is rejected")
}

func TestApplyChangeSets_WriterAErrorSurfaced(t *testing.T) {
	dbB := newMockDB()
	sentinel := errors.New("writerA boom")
	rA, err := NewRoute(newMockDB().reader(), failWriter(sentinel), nil, "evm")
	require.NoError(t, err)
	r, err := NewModuleRouter(rA, newRoute(t, dbB, "bank"))
	require.NoError(t, err)

	applyErr := r.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("evm", kv("k", "v")),
	}, true)
	require.Error(t, applyErr)
	require.ErrorIs(t, applyErr, sentinel)
}

func TestApplyChangeSets_WriterBErrorSurfaced(t *testing.T) {
	dbA := newMockDB()
	sentinel := errors.New("writerB boom")
	rB, err := NewRoute(newMockDB().reader(), failWriter(sentinel), nil, "bank")
	require.NoError(t, err)
	r, err := NewModuleRouter(newRoute(t, dbA, "evm"), rB)
	require.NoError(t, err)

	applyErr := r.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("bank", kv("k", "v")),
	}, true)
	require.Error(t, applyErr)
	require.ErrorIs(t, applyErr, sentinel)
}

func TestApplyChangeSets_BothWritersErrorsJoined(t *testing.T) {
	errA := errors.New("writerA boom")
	errB := errors.New("writerB boom")
	rA, err := NewRoute(newMockDB().reader(), failWriter(errA), nil, "evm")
	require.NoError(t, err)
	rB, err := NewRoute(newMockDB().reader(), failWriter(errB), nil, "bank")
	require.NoError(t, err)
	r, err := NewModuleRouter(rA, rB)
	require.NoError(t, err)

	applyErr := r.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("evm", kv("ek", "ev")),
		namedCS("bank", kv("bk", "bv")),
	}, true)
	require.Error(t, applyErr)
	require.ErrorIs(t, applyErr, errA)
	require.ErrorIs(t, applyErr, errB)
}

// TestModuleRouter_ApplyChangeSets_ErrorContinuesToNextRoute pins the
// contract that a writer returning an error does not abort the loop:
// later writers still run, and the errors from every route are
// aggregated via errors.Join.
func TestModuleRouter_ApplyChangeSets_ErrorContinuesToNextRoute(t *testing.T) {
	errA := errors.New("writerA boom")
	var bCalled bool
	rA, err := NewRoute(newMockDB().reader(), failWriter(errA), nil, "evm")
	require.NoError(t, err)
	rB, err := NewRoute(newMockDB().reader(),
		func(_ []*proto.NamedChangeSet, _ bool) error {
			bCalled = true
			return nil
		}, nil, "bank")
	require.NoError(t, err)
	r, err := NewModuleRouter(rA, rB)
	require.NoError(t, err)

	applyErr := r.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("evm", kv("k", "v")),
	}, true)
	require.ErrorIs(t, applyErr, errA)
	require.True(t, bCalled,
		"an error from route A must not abort the loop; route B's writer still runs")
}

func TestApplyChangeSets_PreservesChangeSetOrderPerDatabase(t *testing.T) {
	r, dbA, dbB := newTestRouter(t, []string{"a1", "a2"}, []string{"b1", "b2"})
	err := r.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("a1", kv("k", "v")),
		namedCS("b1", kv("k", "v")),
		namedCS("a2", kv("k", "v")),
		namedCS("b2", kv("k", "v")),
	}, true)
	require.NoError(t, err)

	require.Len(t, dbA.writeLog, 1)
	require.Len(t, dbB.writeLog, 1)

	names := func(batch []*proto.NamedChangeSet) []string {
		out := make([]string, len(batch))
		for i, cs := range batch {
			out[i] = cs.Name
		}
		return out
	}
	require.Equal(t, []string{"a1", "a2"}, names(dbA.writeLog[0]))
	require.Equal(t, []string{"b1", "b2"}, names(dbB.writeLog[0]))
}

// TestModuleRouter_NestedRouter verifies that a Router's ApplyChangeSets
// method satisfies the DBWriter signature and can be used directly as the
// writer for another Router without any wrapper glue.
func TestModuleRouter_NestedRouter(t *testing.T) {
	// Inner router splits between "a1" and "a2" across dbA1/dbA2.
	dbA1 := newMockDB()
	dbA2 := newMockDB()
	inner, err := NewModuleRouter(
		newRoute(t, dbA1, "a1"),
		newRoute(t, dbA2, "a2"),
	)
	require.NoError(t, err)

	// Outer router routes "a1"/"a2" to the inner router and "b" to dbB.
	dbB := newMockDB()
	innerRoute, err := NewRoute(inner.Read, inner.ApplyChangeSets, inner.GetProof, "a1", "a2")
	require.NoError(t, err)
	outer, err := NewModuleRouter(
		innerRoute,
		newRoute(t, dbB, "b"),
	)
	require.NoError(t, err)

	err = outer.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS("a1", kv("k", "v1")),
		namedCS("a2", kv("k", "v2")),
		namedCS("b", kv("k", "vb")),
	}, true)
	require.NoError(t, err)

	v, ok := dbA1.get("a1", "k")
	require.True(t, ok)
	require.Equal(t, []byte("v1"), v)
	v, ok = dbA2.get("a2", "k")
	require.True(t, ok)
	require.Equal(t, []byte("v2"), v)
	v, ok = dbB.get("b", "k")
	require.True(t, ok)
	require.Equal(t, []byte("vb"), v)

	// Reads nest too.
	v, ok, err = outer.Read("a2", []byte("k"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("v2"), v)
}

// Guards against regressions where construction validation would skip past
// overlap detection because one side's map was scanned.
func TestNewModuleRouter_OverlapDetectedRegardlessOfIterationOrder(t *testing.T) {
	dbA := newMockDB()
	dbB := newMockDB()
	// Use enough modules that iteration order is non-trivial.
	aMods := []string{"m1", "m2", "m3", "m4", "m5"}
	bMods := []string{"n1", "n2", "n3", "m3", "n5"} // m3 overlaps
	r, err := NewModuleRouter(
		newRoute(t, dbA, aMods...),
		newRoute(t, dbB, bMods...),
	)
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), fmt.Sprintf("%q", "m3"))
}

// --- GetProof tests ---

func recordingProofBuilder(p *ics23.CommitmentProof, retErr error) (DBProofBuilder, *[]struct {
	store string
	key   []byte
}) {
	var calls []struct {
		store string
		key   []byte
	}
	b := func(store string, key []byte) (*ics23.CommitmentProof, error) {
		calls = append(calls, struct {
			store string
			key   []byte
		}{store: store, key: key})
		return p, retErr
	}
	return b, &calls
}

func TestGetProof_RoutesToOwningRoute(t *testing.T) {
	dbA := newMockDB()
	dbB := newMockDB()
	wantProof := &ics23.CommitmentProof{}
	otherProof := &ics23.CommitmentProof{}
	proofA, callsA := recordingProofBuilder(wantProof, nil)
	proofB, callsB := recordingProofBuilder(otherProof, nil)

	r, err := NewModuleRouter(
		newRouteWithBuilders(t, dbA, proofA, "evm"),
		newRouteWithBuilders(t, dbB, proofB, "bank"),
	)
	require.NoError(t, err)

	got, err := r.GetProof("evm", []byte("k"))
	require.NoError(t, err)
	require.Same(t, wantProof, got, "must return the exact proof from the matching route's builder")

	require.Len(t, *callsA, 1)
	require.Equal(t, "evm", (*callsA)[0].store)
	require.Equal(t, []byte("k"), (*callsA)[0].key)
	require.Empty(t, *callsB, "non-owning route's builder must not be invoked")
}

func TestGetProof_UnregisteredModuleReturnsError(t *testing.T) {
	dbA := newMockDB()
	proof, calls := recordingProofBuilder(&ics23.CommitmentProof{}, nil)
	r, err := NewModuleRouter(newRouteWithBuilders(t, dbA, proof, "evm"))
	require.NoError(t, err)

	got, err := r.GetProof("staking", []byte("k"))
	require.Error(t, err)
	require.Nil(t, got)
	require.Contains(t, err.Error(), "staking")
	require.Contains(t, err.Error(), "not registered")
	require.Empty(t, *calls, "unregistered store must not invoke any builder")
}

func TestGetProof_NoBuilderReturnsError(t *testing.T) {
	dbA := newMockDB()
	r, err := NewModuleRouter(newRouteWithBuilders(t, dbA, nil, "evm"))
	require.NoError(t, err)

	got, err := r.GetProof("evm", []byte("k"))
	require.Error(t, err)
	require.Nil(t, got)
	require.Contains(t, err.Error(), "evm")
	require.Contains(t, err.Error(), "not supported")
	require.NotContains(t, err.Error(), "not registered",
		"registered-but-unsupported must not be reported as unregistered")
}

func TestGetProof_BuilderErrorPropagated(t *testing.T) {
	dbA := newMockDB()
	sentinel := errors.New("proof boom")
	proof, _ := recordingProofBuilder(nil, sentinel)
	r, err := NewModuleRouter(newRouteWithBuilders(t, dbA, proof, "evm"))
	require.NoError(t, err)

	got, err := r.GetProof("evm", []byte("k"))
	require.ErrorIs(t, err, sentinel)
	require.Nil(t, got)
}

func TestGetProof_PartialBuilderSupportPerRoute(t *testing.T) {
	dbA := newMockDB()
	dbB := newMockDB()
	proof, calls := recordingProofBuilder(&ics23.CommitmentProof{}, nil)
	r, err := NewModuleRouter(
		newRouteWithBuilders(t, dbA, proof, "evm"),
		newRouteWithBuilders(t, dbB, nil, "bank"),
	)
	require.NoError(t, err)

	_, err = r.GetProof("evm", []byte("k"))
	require.NoError(t, err)
	require.Len(t, *calls, 1)

	got, err := r.GetProof("bank", []byte("k"))
	require.Error(t, err)
	require.Nil(t, got)
	require.Contains(t, err.Error(), "bank")
	require.Contains(t, err.Error(), "not supported")
}

// TestModuleRouter_NestedRouter_Proof verifies that wiring a Router's
// GetProof method through an outer Route works the same way
// ApplyChangeSets does: dispatch passes through the inner router
// transparently and reaches the right backing route.
func TestModuleRouter_NestedRouter_Proof(t *testing.T) {
	dbInner := newMockDB()
	innerProof := &ics23.CommitmentProof{}
	proof, proofCalls := recordingProofBuilder(innerProof, nil)

	inner, err := NewModuleRouter(
		newRouteWithBuilders(t, dbInner, proof, "evm"),
	)
	require.NoError(t, err)

	dbOther := newMockDB()
	innerRoute, err := NewRoute(inner.Read, inner.ApplyChangeSets, inner.GetProof, "evm")
	require.NoError(t, err)
	outer, err := NewModuleRouter(
		innerRoute,
		newRoute(t, dbOther, "bank"),
	)
	require.NoError(t, err)

	gotProof, err := outer.GetProof("evm", []byte("k"))
	require.NoError(t, err)
	require.Same(t, innerProof, gotProof)
	require.Len(t, *proofCalls, 1)

	// GetProof against the sibling route (no builder) must still return
	// the documented "not supported" error, even when reached through
	// the outer router.
	_, err = outer.GetProof("bank", []byte("k"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}
