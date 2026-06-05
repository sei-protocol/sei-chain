package historical

import (
	"bytes"
	"context"
	"errors"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

const (
	defaultHistoricalReadCacheEntries = 64 * 1024
	maxHistoricalReadCacheValueBytes  = 64 * 1024
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
}

// FallbackStateStore routes pruned point reads to the historical reader.
// Iteration and writes stay on the primary state store.
type FallbackStateStore struct {
	primary types.StateStore
	reader  Reader
	cache   *lru.Cache[historicalReadCacheKey, historicalReadCacheValue]
}

var _ types.StateStore = (*FallbackStateStore)(nil)

// NewFallbackStateStore takes ownership of primary and reader for Close.
func NewFallbackStateStore(primary types.StateStore, reader Reader) *FallbackStateStore {
	cache, err := lru.New[historicalReadCacheKey, historicalReadCacheValue](defaultHistoricalReadCacheEntries)
	if err != nil {
		panic(err)
	}
	return &FallbackStateStore{primary: primary, reader: reader, cache: cache}
}

func (s *FallbackStateStore) shouldFallback(version int64) bool {
	earliest := s.primary.GetEarliestVersion()
	return earliest > 0 && version < earliest
}

func (s *FallbackStateStore) Get(storeKey string, version int64, key []byte) ([]byte, error) {
	if !s.shouldFallback(version) {
		return s.primary.Get(storeKey, version, key)
	}
	cacheKey := historicalReadCacheKey{storeKey: storeKey, version: version, key: string(key)}
	if value, found, ok := s.getCachedValue(cacheKey); ok {
		if !found {
			return nil, nil
		}
		return value, nil
	}
	v, err := s.reader.Get(context.Background(), storeKey, key, version)
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
	return value.found, true
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
	s.cache.Add(key, historicalReadCacheValue{valueKnown: true})
}

func (s *FallbackStateStore) cacheHas(key historicalReadCacheKey) {
	if s.cache == nil {
		return
	}
	s.cache.Add(key, historicalReadCacheValue{found: true})
}

func (s *FallbackStateStore) Has(storeKey string, version int64, key []byte) (bool, error) {
	if !s.shouldFallback(version) {
		return s.primary.Has(storeKey, version, key)
	}
	cacheKey := historicalReadCacheKey{storeKey: storeKey, version: version, key: string(key)}
	if found, ok := s.getCachedHas(cacheKey); ok {
		return found, nil
	}
	found, err := s.reader.Has(context.Background(), storeKey, key, version)
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

func (s *FallbackStateStore) Iterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	return s.primary.Iterator(storeKey, version, start, end)
}

func (s *FallbackStateStore) ReverseIterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
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
	primaryErr := s.primary.Close()
	readerErr := s.reader.Close()
	if primaryErr != nil {
		return primaryErr
	}
	return readerErr
}
