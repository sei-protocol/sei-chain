package historical

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
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

func (f *fakeStateStore) Iterator(string, int64, []byte, []byte) (dbm.Iterator, error) {
	return nil, nil
}

func (f *fakeStateStore) ReverseIterator(string, int64, []byte, []byte) (dbm.Iterator, error) {
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
	gets      int
	has       int
	getErr    error
	hasResult bool
	hasSet    bool
	// lastVersion overrides the reported ingestion watermark; zero means
	// "fully caught up" so coverage gating stays out of unrelated tests.
	lastVersion  int64
	lastVersions int
}

func (f *fakeReader) Get(context.Context, string, []byte, int64) (Value, error) {
	f.gets++
	if f.getErr != nil {
		return Value{}, f.getErr
	}
	return Value{Bytes: []byte("historical"), Version: 7}, nil
}

func (f *fakeReader) Has(context.Context, string, []byte, int64) (bool, error) {
	f.has++
	if f.hasSet {
		return f.hasResult, nil
	}
	return true, nil
}

func (f *fakeReader) LastVersion(context.Context) (int64, error) {
	f.lastVersions++
	if f.lastVersion != 0 {
		return f.lastVersion, nil
	}
	return 1 << 62, nil
}
func (f *fakeReader) Close() error { return nil }

func TestFallbackStateStoreRoutesPrunedPointReads(t *testing.T) {
	primary := &fakeStateStore{earliest: 10}
	reader := &fakeReader{}
	store := NewFallbackStateStore(primary, reader, 0)

	value, err := store.Get("bank", 7, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("historical"), value)
	require.Equal(t, 0, primary.gets)
	require.Equal(t, 1, reader.gets)

	ok, err := store.Has("bank", 7, []byte("k"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 0, primary.has)
	require.Equal(t, 0, reader.has)
}

func TestFallbackStateStoreKeepsRecentPointReadsOnPrimary(t *testing.T) {
	primary := &fakeStateStore{earliest: 10}
	reader := &fakeReader{}
	store := NewFallbackStateStore(primary, reader, 0)

	value, err := store.Get("bank", 10, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("primary"), value)
	require.Equal(t, 1, primary.gets)
	require.Equal(t, 0, reader.gets)
}

func TestFallbackStateStoreCachesHistoricalPointReads(t *testing.T) {
	primary := &fakeStateStore{earliest: 10}
	reader := &fakeReader{}
	store := NewFallbackStateStore(primary, reader, 0)

	value, err := store.Get("bank", 7, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("historical"), value)
	value[0] = 'H'

	value, err = store.Get("bank", 7, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("historical"), value)
	value[0] = 'H'

	value, err = store.Get("bank", 7, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("historical"), value)
	require.Equal(t, 1, reader.gets)
}

func TestFallbackStateStoreCachesHistoricalMisses(t *testing.T) {
	primary := &fakeStateStore{earliest: 10}
	reader := &fakeReader{getErr: ErrNotFound, hasSet: true}
	store := NewFallbackStateStore(primary, reader, 0)

	value, err := store.Get("bank", 7, []byte("missing"))
	require.NoError(t, err)
	require.Nil(t, value)

	value, err = store.Get("bank", 7, []byte("missing"))
	require.NoError(t, err)
	require.Nil(t, value)

	ok, err := store.Has("bank", 7, []byte("missing"))
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, 1, reader.gets)
	require.Equal(t, 0, reader.has)
}

func TestFallbackStateStoreCachesHistoricalHasResults(t *testing.T) {
	primary := &fakeStateStore{earliest: 10}
	reader := &fakeReader{hasResult: true, hasSet: true}
	store := NewFallbackStateStore(primary, reader, 0)

	ok, err := store.Has("bank", 7, []byte("k"))
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = store.Has("bank", 7, []byte("k"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 1, reader.has)
}

func TestFallbackStateStoreDoesNotUseHasOnlyCacheForGet(t *testing.T) {
	primary := &fakeStateStore{earliest: 10}
	reader := &fakeReader{hasResult: true, hasSet: true}
	store := NewFallbackStateStore(primary, reader, 0)

	ok, err := store.Has("bank", 7, []byte("k"))
	require.NoError(t, err)
	require.True(t, ok)

	value, err := store.Get("bank", 7, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("historical"), value)
	require.Equal(t, 1, reader.gets)
}

type fakePerKeyStateStore struct {
	fakeStateStore
	perKey map[string]int64
}

func (f *fakePerKeyStateStore) GetEarliestVersionForKey(storeKey string) int64 {
	if v, ok := f.perKey[storeKey]; ok {
		return v
	}
	return f.earliest
}

func TestFallbackStateStoreUsesPerKeyEarliestVersion(t *testing.T) {
	primary := &fakePerKeyStateStore{
		fakeStateStore: fakeStateStore{earliest: 10},
		perKey:         map[string]int64{"evm": 5},
	}
	reader := &fakeReader{}
	store := NewFallbackStateStore(primary, reader, 0)

	// The EVM store still holds version 7 locally even though the cosmos store
	// pruned to 10; the read must stay on the primary.
	value, err := store.Get("evm", 7, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("primary"), value)
	require.Equal(t, 1, primary.gets)
	require.Equal(t, 0, reader.gets)

	// Other store keys fall back below the cosmos horizon as before.
	value, err = store.Get("bank", 7, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("historical"), value)
	require.Equal(t, 1, reader.gets)

	ok, err := store.Has("evm", 7, []byte("k"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 1, primary.has)
	require.Equal(t, 0, reader.has)
}

func TestFallbackStateStoreCoverageFloor(t *testing.T) {
	primary := &fakeStateStore{earliest: 100}
	reader := &fakeReader{}
	store := NewFallbackStateStore(primary, reader, 50)

	// The floor becomes the advertised earliest so height gates admit pruned
	// heights the fallback can serve.
	require.Equal(t, int64(50), store.GetEarliestVersion())

	// In [floor, local earliest): fallback serves the read.
	value, err := store.Get("bank", 60, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("historical"), value)
	require.Equal(t, 1, reader.gets)

	// Below the floor: the backend has no coverage; stay on the primary.
	value, err = store.Get("bank", 49, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("primary"), value)
	require.Equal(t, 1, reader.gets)

	// Negative versions never fall back.
	_, err = store.Get("bank", -1, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, 1, reader.gets)
}

func TestFallbackStateStoreRejectsReadsBeyondBackendCoverage(t *testing.T) {
	primary := &fakeStateStore{earliest: 100}
	reader := &fakeReader{lastVersion: 40}
	store := NewFallbackStateStore(primary, reader, 0)

	// The backend has only ingested up to 40; a pruned read at 90 must error
	// instead of returning silently-empty state.
	_, err := store.Get("bank", 90, []byte("k"))
	require.ErrorContains(t, err, "behind requested version")
	require.Equal(t, 0, reader.gets)

	_, err = store.Has("bank", 90, []byte("k"))
	require.ErrorContains(t, err, "behind requested version")
	require.Equal(t, 0, reader.has)

	// The watermark refresh is rate-limited: the second miss must not trigger
	// another LastVersion call.
	require.Equal(t, 1, reader.lastVersions)

	// Reads at or below the ingested watermark are served.
	value, err := store.Get("bank", 40, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("historical"), value)
	require.Equal(t, 1, reader.gets)
}

func TestFallbackStateStoreExpiresCachedMisses(t *testing.T) {
	primary := &fakeStateStore{earliest: 10}
	reader := &fakeReader{getErr: ErrNotFound}
	store := NewFallbackStateStore(primary, reader, 0)

	value, err := store.Get("bank", 7, []byte("missing"))
	require.NoError(t, err)
	require.Nil(t, value)
	require.Equal(t, 1, reader.gets)

	// Backdate the cached miss to simulate the TTL lapsing after the offload
	// consumer catches up.
	cacheKey := readCacheKey{storeKey: "bank", version: 7, key: "missing"}
	entry, ok := store.cache.Get(cacheKey)
	require.True(t, ok)
	entry.missExpiresAt = time.Now().Add(-time.Second)
	store.cache.Add(cacheKey, entry)

	reader.getErr = nil
	value, err = store.Get("bank", 7, []byte("missing"))
	require.NoError(t, err)
	require.Equal(t, []byte("historical"), value)
	require.Equal(t, 2, reader.gets)
}

func TestFallbackStateStoreDoesNotCacheHistoricalErrors(t *testing.T) {
	primary := &fakeStateStore{earliest: 10}
	reader := &fakeReader{getErr: errors.New("boom")}
	store := NewFallbackStateStore(primary, reader, 0)

	_, err := store.Get("bank", 7, []byte("k"))
	require.Error(t, err)

	reader.getErr = nil
	value, err := store.Get("bank", 7, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("historical"), value)
	require.Equal(t, 2, reader.gets)
}
