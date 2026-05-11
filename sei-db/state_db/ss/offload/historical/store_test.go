package historical

import (
	"context"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

type fakeStateStore struct {
	earliest int64
	gets     int
	has      int
}

func (f *fakeStateStore) Get(_ string, _ int64, _ []byte) ([]byte, error) {
	f.gets++
	return []byte("primary"), nil
}

func (f *fakeStateStore) Has(_ string, _ int64, _ []byte) (bool, error) {
	f.has++
	return true, nil
}

func (f *fakeStateStore) Iterator(string, int64, []byte, []byte) (types.DBIterator, error) {
	return nil, nil
}

func (f *fakeStateStore) ReverseIterator(string, int64, []byte, []byte) (types.DBIterator, error) {
	return nil, nil
}

func (f *fakeStateStore) RawIterate(string, func([]byte, []byte, int64) bool) (bool, error) {
	return false, nil
}

func (f *fakeStateStore) GetLatestVersion() int64      { return 0 }
func (f *fakeStateStore) SetLatestVersion(int64) error { return nil }
func (f *fakeStateStore) GetEarliestVersion() int64    { return f.earliest }
func (f *fakeStateStore) SetEarliestVersion(version int64, _ bool) error {
	f.earliest = version
	return nil
}
func (f *fakeStateStore) ApplyChangesetSync(int64, []*proto.NamedChangeSet) error  { return nil }
func (f *fakeStateStore) ApplyChangesetAsync(int64, []*proto.NamedChangeSet) error { return nil }
func (f *fakeStateStore) Prune(int64) error                                        { return nil }
func (f *fakeStateStore) Import(int64, <-chan types.SnapshotNode) error            { return nil }
func (f *fakeStateStore) Close() error                                             { return nil }

type fakeReader struct {
	gets int
	has  int
}

func (f *fakeReader) Get(context.Context, string, []byte, int64) (Value, error) {
	f.gets++
	return Value{Bytes: []byte("historical"), Version: 7}, nil
}

func (f *fakeReader) Has(context.Context, string, []byte, int64) (bool, error) {
	f.has++
	return true, nil
}

func (f *fakeReader) BatchGet(context.Context, int64, []Lookup) (map[Lookup]Value, error) {
	return nil, nil
}

func (f *fakeReader) LastVersion(context.Context) (int64, error) { return 0, nil }
func (f *fakeReader) Close() error                               { return nil }

func TestFallbackStateStoreRoutesPrunedPointReads(t *testing.T) {
	primary := &fakeStateStore{earliest: 10}
	reader := &fakeReader{}
	store := NewFallbackStateStore(primary, reader)

	value, err := store.Get("bank", 7, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("historical"), value)
	require.Equal(t, 0, primary.gets)
	require.Equal(t, 1, reader.gets)

	ok, err := store.Has("bank", 7, []byte("k"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 0, primary.has)
	require.Equal(t, 1, reader.has)
}

func TestFallbackStateStoreKeepsRecentPointReadsOnPrimary(t *testing.T) {
	primary := &fakeStateStore{earliest: 10}
	reader := &fakeReader{}
	store := NewFallbackStateStore(primary, reader)

	value, err := store.Get("bank", 10, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("primary"), value)
	require.Equal(t, 1, primary.gets)
	require.Equal(t, 0, reader.gets)
}
