package historical

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// fakePrimary is a minimal types.StateStore implementation for routing tests.
// Only the calls FallbackStateStore actually makes are populated; the rest
// return zero values, which keeps the test file focused on routing logic.
type fakePrimary struct {
	earliest int64
	latest   int64
	gets     map[string][]byte // storeKey|key -> value (nil means not present)
	getCalls int
	hasCalls int
	closed   bool
}

func newFakePrimary(earliest, latest int64) *fakePrimary {
	return &fakePrimary{earliest: earliest, latest: latest, gets: map[string][]byte{}}
}

func k(storeKey string, key []byte) string { return storeKey + "|" + string(key) }

func (f *fakePrimary) Get(storeKey string, _ int64, key []byte) ([]byte, error) {
	f.getCalls++
	return f.gets[k(storeKey, key)], nil
}
func (f *fakePrimary) Has(storeKey string, _ int64, key []byte) (bool, error) {
	f.hasCalls++
	return f.gets[k(storeKey, key)] != nil, nil
}
func (f *fakePrimary) Iterator(string, int64, []byte, []byte) (types.DBIterator, error) {
	return nil, nil
}
func (f *fakePrimary) ReverseIterator(string, int64, []byte, []byte) (types.DBIterator, error) {
	return nil, nil
}
func (f *fakePrimary) RawIterate(string, func([]byte, []byte, int64) bool) (bool, error) {
	return false, nil
}
func (f *fakePrimary) GetLatestVersion() int64                                  { return f.latest }
func (f *fakePrimary) SetLatestVersion(int64) error                             { return nil }
func (f *fakePrimary) GetEarliestVersion() int64                                { return f.earliest }
func (f *fakePrimary) SetEarliestVersion(int64, bool) error                     { return nil }
func (f *fakePrimary) ApplyChangesetSync(int64, []*proto.NamedChangeSet) error  { return nil }
func (f *fakePrimary) ApplyChangesetAsync(int64, []*proto.NamedChangeSet) error { return nil }
func (f *fakePrimary) Prune(int64) error                                        { return nil }
func (f *fakePrimary) Import(int64, <-chan types.SnapshotNode) error            { return nil }
func (f *fakePrimary) Close() error                                             { f.closed = true; return nil }

// fakeReader implements Reader for routing tests. It records call counts so
// each test can assert that fallback (or non-fallback) actually happened.
type fakeReader struct {
	values    map[Lookup]Value
	getCalls  int
	closeCall bool
}

func newFakeReader() *fakeReader { return &fakeReader{values: map[Lookup]Value{}} }

func (r *fakeReader) Get(_ context.Context, storeName string, key []byte, _ int64) (Value, error) {
	r.getCalls++
	v, ok := r.values[Lookup{StoreName: storeName, Key: string(key)}]
	if !ok {
		return Value{}, ErrNotFound
	}
	return v, nil
}
func (r *fakeReader) BatchGet(context.Context, int64, []Lookup) (map[Lookup]Value, error) {
	return nil, nil
}
func (r *fakeReader) LastVersion(context.Context) (int64, error) { return 0, nil }
func (r *fakeReader) Close() error                               { r.closeCall = true; return nil }

func TestFallbackRoutesBelowEarliest(t *testing.T) {
	p := newFakePrimary(100, 200)
	r := newFakeReader()
	r.values[Lookup{StoreName: "evm", Key: "k"}] = Value{Bytes: []byte("from-cockroach"), Version: 50}
	s := NewFallbackStateStore(p, r)

	got, err := s.Get("evm", 50, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("from-cockroach"), got)
	require.Equal(t, 1, r.getCalls)
	require.Equal(t, 0, p.getCalls, "primary should not be consulted below earliest")
}

func TestFallbackUsesPrimaryAtOrAboveEarliest(t *testing.T) {
	p := newFakePrimary(100, 200)
	p.gets[k("evm", []byte("k"))] = []byte("from-primary")
	r := newFakeReader()
	s := NewFallbackStateStore(p, r)

	for _, version := range []int64{100, 150, 200} {
		got, err := s.Get("evm", version, []byte("k"))
		require.NoError(t, err)
		require.Equal(t, []byte("from-primary"), got, "version=%d should hit primary", version)
	}
	require.Equal(t, 3, p.getCalls)
	require.Equal(t, 0, r.getCalls, "reader should not be consulted at or above earliest")
}

func TestFallbackUsesPrimaryWhenEarliestIsZero(t *testing.T) {
	// earliest=0 means the primary has no data yet (or was never pruned). We
	// shouldn't fan out to Cockroach in that case — the primary owns it.
	p := newFakePrimary(0, 0)
	r := newFakeReader()
	s := NewFallbackStateStore(p, r)

	_, err := s.Get("evm", 50, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, 1, p.getCalls)
	require.Equal(t, 0, r.getCalls)
}

func TestFallbackHasMirrorsGetRouting(t *testing.T) {
	p := newFakePrimary(100, 200)
	r := newFakeReader()
	r.values[Lookup{StoreName: "bank", Key: "addr"}] = Value{Bytes: []byte{1}, Version: 50}
	s := NewFallbackStateStore(p, r)

	ok, err := s.Has("bank", 50, []byte("addr"))
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = s.Has("bank", 50, []byte("missing"))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestFallbackPropagatesNonNotFoundReaderErrors(t *testing.T) {
	p := newFakePrimary(100, 200)
	r := &errReader{err: errors.New("boom")}
	s := NewFallbackStateStore(p, r)

	_, err := s.Get("evm", 50, []byte("k"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "boom")
}

type errReader struct{ err error }

func (e *errReader) Get(context.Context, string, []byte, int64) (Value, error) {
	return Value{}, e.err
}
func (e *errReader) BatchGet(context.Context, int64, []Lookup) (map[Lookup]Value, error) {
	return nil, e.err
}
func (e *errReader) LastVersion(context.Context) (int64, error) { return 0, e.err }
func (e *errReader) Close() error                               { return nil }

func TestFallbackCloseClosesBoth(t *testing.T) {
	p := newFakePrimary(0, 0)
	r := newFakeReader()
	s := NewFallbackStateStore(p, r)

	require.NoError(t, s.Close())
	require.True(t, p.closed)
	require.True(t, r.closeCall)
}

func TestFallbackPassthroughGettersDelegate(t *testing.T) {
	p := newFakePrimary(123, 456)
	s := NewFallbackStateStore(p, newFakeReader())

	require.Equal(t, int64(123), s.GetEarliestVersion())
	require.Equal(t, int64(456), s.GetLatestVersion())
}
