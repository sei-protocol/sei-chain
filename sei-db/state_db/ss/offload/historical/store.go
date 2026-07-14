package historical

import (
	"bytes"
	"context"
	"errors"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	dbm "github.com/tendermint/tm-db"
)

const (
	defaultHistoricalReadCacheEntries = 64 * 1024
	maxHistoricalReadCacheValueBytes  = 64 * 1024

	// historicalReadTimeout bounds one backend point read. types.StateStore has
	// no context parameter, so the deadline must be injected here; without it a
	// silently dropped connection can park an RPC goroutine in a backend read
	// until the OS TCP timeout.
	historicalReadTimeout = 10 * time.Second

	// historicalMissCacheTTL bounds how long a backend miss is trusted. Hits are
	// immutable (a version's value never changes once written) but a miss can
	// flip to a hit once the offload consumer catches up.
	historicalMissCacheTTL = time.Minute
)

type historicalReadCacheKey struct {
	storeKey string
	version  int64
	key      string
}

type historicalReadCacheValue struct {
	value      []byte
	found      bool
	valueKnown bool
	// missExpiresAt is set only on miss entries; found entries never expire.
	missExpiresAt time.Time
}

// PerKeyEarliestVersioner is implemented by primaries whose stores prune
// independently per store key (e.g. the composite store's cosmos and EVM
// backends), so the fallback horizon can be checked against the store that
// will actually serve the read.
type PerKeyEarliestVersioner interface {
	GetEarliestVersionForKey(storeKey string) int64
}

// FallbackStateStore routes pruned point reads to the historical reader.
// Iteration and writes stay on the primary state store.
type FallbackStateStore struct {
	primary        types.StateStore
	perKeyEarliest PerKeyEarliestVersioner
	reader         Reader
	cache          *lru.Cache[historicalReadCacheKey, historicalReadCacheValue]
}

var _ types.StateStore = (*FallbackStateStore)(nil)

// NewFallbackStateStore takes ownership of primary and reader for Close.
func NewFallbackStateStore(primary types.StateStore, reader Reader) *FallbackStateStore {
	cache, err := lru.New[historicalReadCacheKey, historicalReadCacheValue](defaultHistoricalReadCacheEntries)
	if err != nil {
		panic(err)
	}
	perKeyEarliest, _ := primary.(PerKeyEarliestVersioner)
	return &FallbackStateStore{primary: primary, perKeyEarliest: perKeyEarliest, reader: reader, cache: cache}
}

func (s *FallbackStateStore) shouldFallback(storeKey string, version int64) bool {
	earliest := s.earliestVersionFor(storeKey)
	return earliest > 0 && version < earliest
}

func (s *FallbackStateStore) earliestVersionFor(storeKey string) int64 {
	if s.perKeyEarliest != nil {
		return s.perKeyEarliest.GetEarliestVersionForKey(storeKey)
	}
	return s.primary.GetEarliestVersion()
}

func (s *FallbackStateStore) readContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), historicalReadTimeout)
}

func (s *FallbackStateStore) Get(storeKey string, version int64, key []byte) ([]byte, error) {
	if !s.shouldFallback(storeKey, version) {
		return s.primary.Get(storeKey, version, key)
	}
	cacheKey := historicalReadCacheKey{storeKey: storeKey, version: version, key: string(key)}
	if value, found, ok := s.getCachedValue(cacheKey); ok {
		if !found {
			return nil, nil
		}
		return value, nil
	}
	ctx, cancel := s.readContext()
	defer cancel()
	v, err := s.reader.Get(ctx, storeKey, key, version)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.cacheMiss(cacheKey)
			return nil, nil
		}
		return nil, err
	}
	s.cacheValue(cacheKey, v.Bytes)
	return v.Bytes, nil
}

func (s *FallbackStateStore) getCachedValue(key historicalReadCacheKey) ([]byte, bool, bool) {
	if s.cache == nil {
		return nil, false, false
	}
	value, ok := s.cache.Get(key)
	if !ok {
		return nil, false, false
	}
	if !value.found {
		if s.missExpired(key, value) {
			return nil, false, false
		}
		return nil, false, true
	}
	if !value.valueKnown {
		return nil, false, false
	}
	return bytes.Clone(value.value), true, true
}

func (s *FallbackStateStore) getCachedHas(key historicalReadCacheKey) (bool, bool) {
	if s.cache == nil {
		return false, false
	}
	value, ok := s.cache.Get(key)
	if !ok {
		return false, false
	}
	if !value.found && s.missExpired(key, value) {
		return false, false
	}
	return value.found, true
}

// missExpired evicts and reports miss entries whose TTL has lapsed so a
// backend that has since ingested the version gets re-queried.
func (s *FallbackStateStore) missExpired(key historicalReadCacheKey, value historicalReadCacheValue) bool {
	if value.missExpiresAt.IsZero() || time.Now().Before(value.missExpiresAt) {
		return false
	}
	s.cache.Remove(key)
	return true
}

func (s *FallbackStateStore) cacheValue(key historicalReadCacheKey, value []byte) {
	if s.cache == nil || value == nil || len(value) > maxHistoricalReadCacheValueBytes {
		return
	}
	s.cache.Add(key, historicalReadCacheValue{value: bytes.Clone(value), found: true, valueKnown: true})
}

func (s *FallbackStateStore) cacheMiss(key historicalReadCacheKey) {
	if s.cache == nil {
		return
	}
	s.cache.Add(key, historicalReadCacheValue{valueKnown: true, missExpiresAt: time.Now().Add(historicalMissCacheTTL)})
}

func (s *FallbackStateStore) cacheHas(key historicalReadCacheKey) {
	if s.cache == nil {
		return
	}
	s.cache.Add(key, historicalReadCacheValue{found: true})
}

func (s *FallbackStateStore) Has(storeKey string, version int64, key []byte) (bool, error) {
	if !s.shouldFallback(storeKey, version) {
		return s.primary.Has(storeKey, version, key)
	}
	cacheKey := historicalReadCacheKey{storeKey: storeKey, version: version, key: string(key)}
	if found, ok := s.getCachedHas(cacheKey); ok {
		return found, nil
	}
	ctx, cancel := s.readContext()
	defer cancel()
	found, err := s.reader.Has(ctx, storeKey, key, version)
	if err != nil {
		return false, err
	}
	if found {
		s.cacheHas(cacheKey)
	} else {
		s.cacheMiss(cacheKey)
	}
	return found, nil
}

func (s *FallbackStateStore) Iterator(storeKey string, version int64, start, end []byte) (dbm.Iterator, error) {
	return s.primary.Iterator(storeKey, version, start, end)
}

func (s *FallbackStateStore) ReverseIterator(storeKey string, version int64, start, end []byte) (dbm.Iterator, error) {
	return s.primary.ReverseIterator(storeKey, version, start, end)
}

func (s *FallbackStateStore) RawIterate(storeKey string, fn func([]byte, []byte, int64) bool) (bool, error) {
	return s.primary.RawIterate(storeKey, fn)
}

func (s *FallbackStateStore) GetLatestVersion() int64 { return s.primary.GetLatestVersion() }

func (s *FallbackStateStore) SetLatestVersion(version int64) error {
	return s.primary.SetLatestVersion(version)
}

func (s *FallbackStateStore) GetEarliestVersion() int64 { return s.primary.GetEarliestVersion() }

func (s *FallbackStateStore) SetEarliestVersion(version int64, ignoreVersion bool) error {
	return s.primary.SetEarliestVersion(version, ignoreVersion)
}

func (s *FallbackStateStore) ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error {
	return s.primary.ApplyChangesetSync(version, changesets)
}

func (s *FallbackStateStore) ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error {
	return s.primary.ApplyChangesetAsync(version, changesets)
}

func (s *FallbackStateStore) Prune(version int64) error { return s.primary.Prune(version) }

func (s *FallbackStateStore) Import(version int64, ch <-chan types.SnapshotNode) error {
	return s.primary.Import(version, ch)
}

func (s *FallbackStateStore) Close() error {
	return errors.Join(s.primary.Close(), s.reader.Close())
}
