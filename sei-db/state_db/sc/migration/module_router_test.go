package migration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// modSet is a small helper for building a module-name set literal.
func modSet(names ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(names))
	for _, n := range names {
		out[n] = struct{}{}
	}
	return out
}

// newTestRouter wires up a ModuleRouter backed by two fresh mockDBs and
// returns the router along with the backing DBs so tests can seed state
// and assert on persisted writes.
func newTestRouter(t *testing.T, aModules, bModules map[string]struct{}) (*ModuleRouter, *mockDB, *mockDB) {
	t.Helper()
	dbA := newMockDB()
	dbB := newMockDB()
	r, err := NewModuleRouter(dbA.reader(), dbA.writer(), dbB.reader(), dbB.writer(), aModules, bModules)
	require.NoError(t, err)
	return r, dbA, dbB
}

// --- Constructor tests ---

func TestNewModuleRouter_Success(t *testing.T) {
	dbA := newMockDB()
	dbB := newMockDB()
	r, err := NewModuleRouter(
		dbA.reader(), dbA.writer(),
		dbB.reader(), dbB.writer(),
		modSet("evm"), modSet("bank"),
	)
	require.NoError(t, err)
	require.NotNil(t, r)
}

func TestNewModuleRouter_NilArgumentsRejected(t *testing.T) {
	dbA := newMockDB()
	dbB := newMockDB()
	okA := modSet("evm")
	okB := modSet("bank")

	tests := []struct {
		name    string
		readerA DBReader
		writerA DBWriter
		readerB DBReader
		writerB DBWriter
		aMods   map[string]struct{}
		bMods   map[string]struct{}
		errSub  string
	}{
		{"nil readerA", nil, dbA.writer(), dbB.reader(), dbB.writer(), okA, okB, "readerA"},
		{"nil writerA", dbA.reader(), nil, dbB.reader(), dbB.writer(), okA, okB, "writerA"},
		{"nil readerB", dbA.reader(), dbA.writer(), nil, dbB.writer(), okA, okB, "readerB"},
		{"nil writerB", dbA.reader(), dbA.writer(), dbB.reader(), nil, okA, okB, "writerB"},
		{"nil aModules", dbA.reader(), dbA.writer(), dbB.reader(), dbB.writer(), nil, okB, "aModules"},
		{"nil bModules", dbA.reader(), dbA.writer(), dbB.reader(), dbB.writer(), okA, nil, "bModules"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := NewModuleRouter(tc.readerA, tc.writerA, tc.readerB, tc.writerB, tc.aMods, tc.bMods)
			require.Error(t, err)
			require.Nil(t, r)
			require.Contains(t, err.Error(), tc.errSub)
		})
	}
}

func TestNewModuleRouter_OverlappingModulesRejected(t *testing.T) {
	dbA := newMockDB()
	dbB := newMockDB()
	r, err := NewModuleRouter(
		dbA.reader(), dbA.writer(),
		dbB.reader(), dbB.writer(),
		modSet("evm", "shared"), modSet("bank", "shared"),
	)
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "shared")
	require.Contains(t, err.Error(), "both")
}

func TestNewModuleRouter_EmptyModuleSetsAllowed(t *testing.T) {
	// Empty (but non-nil) module sets should be accepted. Any read or
	// write will then error, but construction itself is fine.
	dbA := newMockDB()
	dbB := newMockDB()
	r, err := NewModuleRouter(
		dbA.reader(), dbA.writer(),
		dbB.reader(), dbB.writer(),
		modSet(), modSet(),
	)
	require.NoError(t, err)
	require.NotNil(t, r)
}

// --- Read tests ---

func TestRead_RoutesToA(t *testing.T) {
	r, dbA, _ := newTestRouter(t, modSet("evm"), modSet("bank"))
	dbA.seed(map[string]map[string][]byte{
		"evm": {"k1": []byte("v1")},
	})

	val, ok, err := r.Read("evm", []byte("k1"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("v1"), val)
}

func TestRead_RoutesToB(t *testing.T) {
	r, _, dbB := newTestRouter(t, modSet("evm"), modSet("bank"))
	dbB.seed(map[string]map[string][]byte{
		"bank": {"k2": []byte("v2")},
	})

	val, ok, err := r.Read("bank", []byte("k2"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("v2"), val)
}

func TestRead_MissingKeyReturnsNotFound(t *testing.T) {
	r, _, _ := newTestRouter(t, modSet("evm"), modSet("bank"))
	val, ok, err := r.Read("evm", []byte("missing"))
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, val)
}

func TestRead_UnregisteredModuleReturnsError(t *testing.T) {
	r, _, _ := newTestRouter(t, modSet("evm"), modSet("bank"))
	val, ok, err := r.Read("staking", []byte("k1"))
	require.Error(t, err)
	require.False(t, ok)
	require.Nil(t, val)
	require.Contains(t, err.Error(), "staking")
}

func TestRead_DoesNotFallThroughBetweenDatabases(t *testing.T) {
	// A value with the same key existing in DB B must not be returned
	// when the store is routed to DB A.
	r, _, dbB := newTestRouter(t, modSet("evm"), modSet("bank"))
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
	r, err := NewModuleRouter(
		failReader(sentinel), func(_ []*proto.NamedChangeSet) error { return nil },
		dbB.reader(), dbB.writer(),
		modSet("evm"), modSet("bank"),
	)
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
	r, dbA, dbB := newTestRouter(t, modSet("evm", "wasm"), modSet("bank", "staking"))
	err := r.ApplyChangeSets(context.Background(), []*proto.NamedChangeSet{
		namedCS("evm", kv("ek", "ev")),
		namedCS("bank", kv("bk", "bv")),
		namedCS("wasm", kv("wk", "wv")),
		namedCS("staking", kv("sk", "sv")),
	})
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

func TestApplyChangeSets_DeletePropagatesToCorrectDatabase(t *testing.T) {
	r, dbA, dbB := newTestRouter(t, modSet("evm"), modSet("bank"))
	dbA.seed(map[string]map[string][]byte{"evm": {"k1": []byte("v1")}})
	dbB.seed(map[string]map[string][]byte{"bank": {"k2": []byte("v2")}})

	err := r.ApplyChangeSets(context.Background(), []*proto.NamedChangeSet{
		namedCS("evm", del("k1")),
		namedCS("bank", del("k2")),
	})
	require.NoError(t, err)

	_, ok := dbA.get("evm", "k1")
	require.False(t, ok)
	_, ok = dbB.get("bank", "k2")
	require.False(t, ok)
}

func TestApplyChangeSets_EmptyAndNilInputs(t *testing.T) {
	r, dbA, dbB := newTestRouter(t, modSet("evm"), modSet("bank"))

	require.NoError(t, r.ApplyChangeSets(context.Background(), nil))
	require.NoError(t, r.ApplyChangeSets(context.Background(), []*proto.NamedChangeSet{}))
	require.NoError(t, r.ApplyChangeSets(context.Background(), []*proto.NamedChangeSet{nil, nil}))

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
	r, dbA, dbB := newTestRouter(t, modSet("evm"), modSet("bank"))
	err := r.ApplyChangeSets(context.Background(), []*proto.NamedChangeSet{
		namedCS("evm", kv("ek", "ev")),
		namedCS("staking", kv("sk", "sv")),
		namedCS("bank", kv("bk", "bv")),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "staking")

	// Validation runs up front, so no writes should have been dispatched.
	require.Empty(t, dbA.writeLog, "no writes should land in dbA when the batch is rejected")
	require.Empty(t, dbB.writeLog, "no writes should land in dbB when the batch is rejected")
}

func TestApplyChangeSets_WriterAErrorSurfaced(t *testing.T) {
	dbB := newMockDB()
	sentinel := errors.New("writerA boom")
	r, err := NewModuleRouter(
		newMockDB().reader(), failWriter(sentinel),
		dbB.reader(), dbB.writer(),
		modSet("evm"), modSet("bank"),
	)
	require.NoError(t, err)

	applyErr := r.ApplyChangeSets(context.Background(), []*proto.NamedChangeSet{
		namedCS("evm", kv("k", "v")),
	})
	require.Error(t, applyErr)
	require.ErrorIs(t, applyErr, sentinel)
	require.Contains(t, applyErr.Error(), "database A")
}

func TestApplyChangeSets_WriterBErrorSurfaced(t *testing.T) {
	dbA := newMockDB()
	sentinel := errors.New("writerB boom")
	r, err := NewModuleRouter(
		dbA.reader(), dbA.writer(),
		newMockDB().reader(), failWriter(sentinel),
		modSet("evm"), modSet("bank"),
	)
	require.NoError(t, err)

	applyErr := r.ApplyChangeSets(context.Background(), []*proto.NamedChangeSet{
		namedCS("bank", kv("k", "v")),
	})
	require.Error(t, applyErr)
	require.ErrorIs(t, applyErr, sentinel)
	require.Contains(t, applyErr.Error(), "database B")
}

func TestApplyChangeSets_BothWritersErrorsJoined(t *testing.T) {
	errA := errors.New("writerA boom")
	errB := errors.New("writerB boom")
	r, err := NewModuleRouter(
		newMockDB().reader(), failWriter(errA),
		newMockDB().reader(), failWriter(errB),
		modSet("evm"), modSet("bank"),
	)
	require.NoError(t, err)

	applyErr := r.ApplyChangeSets(context.Background(), []*proto.NamedChangeSet{
		namedCS("evm", kv("ek", "ev")),
		namedCS("bank", kv("bk", "bv")),
	})
	require.Error(t, applyErr)
	require.ErrorIs(t, applyErr, errA)
	require.ErrorIs(t, applyErr, errB)
}

func TestModuleRouter_ApplyChangeSets_AlreadyCancelledContext(t *testing.T) {
	// Use a writer that blocks forever so the only way ApplyChangeSets
	// can return is via ctx.Done().
	blockForever := func(_ []*proto.NamedChangeSet) error {
		select {}
	}
	r, err := NewModuleRouter(
		newMockDB().reader(), blockForever,
		newMockDB().reader(), blockForever,
		modSet("evm"), modSet("bank"),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		done <- r.ApplyChangeSets(ctx, []*proto.NamedChangeSet{
			namedCS("evm", kv("k", "v")),
		})
	}()
	select {
	case applyErr := <-done:
		require.ErrorIs(t, applyErr, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("ApplyChangeSets did not return with a pre-cancelled ctx")
	}
}

func TestModuleRouter_ApplyChangeSets_ContextCancellationReturnsError(t *testing.T) {
	// Use writers that block on a channel so we can cancel the context
	// mid-call and assert that ApplyChangeSets returns ctx.Err().
	release := make(chan struct{})
	block := func(_ []*proto.NamedChangeSet) error {
		<-release
		return nil
	}
	r, err := NewModuleRouter(
		newMockDB().reader(), block,
		newMockDB().reader(), block,
		modSet("evm"), modSet("bank"),
	)
	require.NoError(t, err)
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- r.ApplyChangeSets(ctx, []*proto.NamedChangeSet{
			namedCS("evm", kv("k", "v")),
			namedCS("bank", kv("k", "v")),
		})
	}()

	// Give the goroutines a chance to enter the blocked writers.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case applyErr := <-done:
		require.ErrorIs(t, applyErr, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("ApplyChangeSets did not return after ctx cancellation")
	}
}

func TestApplyChangeSets_WritersRunInParallel(t *testing.T) {
	// Use a synchronization point both writers must reach before either
	// may return. If the router ran them sequentially, this would
	// deadlock and the test would time out.
	var wg sync.WaitGroup
	wg.Add(2)
	gate := func(_ []*proto.NamedChangeSet) error {
		wg.Done()
		wg.Wait()
		return nil
	}
	r, err := NewModuleRouter(
		newMockDB().reader(), gate,
		newMockDB().reader(), gate,
		modSet("evm"), modSet("bank"),
	)
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		done <- r.ApplyChangeSets(context.Background(), []*proto.NamedChangeSet{
			namedCS("evm", kv("k", "v")),
			namedCS("bank", kv("k", "v")),
		})
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("ApplyChangeSets did not finish; writers likely ran sequentially")
	}
}

func TestApplyChangeSets_PreservesChangeSetOrderPerDatabase(t *testing.T) {
	r, dbA, dbB := newTestRouter(t, modSet("a1", "a2"), modSet("b1", "b2"))
	err := r.ApplyChangeSets(context.Background(), []*proto.NamedChangeSet{
		namedCS("a1", kv("k", "v")),
		namedCS("b1", kv("k", "v")),
		namedCS("a2", kv("k", "v")),
		namedCS("b2", kv("k", "v")),
	})
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

// Guards against regressions where construction validation would skip past
// overlap detection because one side's map was scanned.
func TestNewModuleRouter_OverlapDetectedRegardlessOfIterationOrder(t *testing.T) {
	dbA := newMockDB()
	dbB := newMockDB()
	// Use enough modules that iteration order is non-trivial.
	aMods := modSet("m1", "m2", "m3", "m4", "m5")
	bMods := modSet("n1", "n2", "n3", "m3", "n5") // m3 overlaps
	r, err := NewModuleRouter(
		dbA.reader(), dbA.writer(),
		dbB.reader(), dbB.writer(),
		aMods, bMods,
	)
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), fmt.Sprintf("%q", "m3"))
}
