package historical

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	dbm "github.com/tendermint/tm-db"
)

const (
	// Cache entries and the per-value cap bound worst-case memory at
	// entries*cap (256 MiB) even when an external caller crafts distinct
	// large-value historical queries.
	defaultHistoricalReadCacheEntries = 32 * 1024
	maxHistoricalReadCacheValueBytes  = 8 * 1024

	// historicalReadTimeout bounds one backend point read. types.StateStore has
	// no context parameter, so the deadline must be injected here; without it a
	// silently dropped connection can park an RPC goroutine in a backend read
	// until the OS TCP timeout.
	historicalReadTimeout = 10 * time.Second

	// historicalMissCacheTTL bounds how long a backend miss is trusted. Hits are
	// immutable (a version's value never changes once written) but a miss can
	// flip to a hit once the offload consumer catches up.
	historicalMissCacheTTL = time.Minute

	// backendVersionRecheckInterval rate-limits LastVersion refreshes when a
	// requested version is ahead of the backend's cached ingestion watermark.
	backendVersionRecheckInterval = 30 * time.Second
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

// FallbackOptions tunes FallbackStateStore behavior.
type FallbackOptions struct {
	// EarliestVersion is the operator-declared earliest version (inclusive)
	// fully ingested into the historical backend. When set (> 0) it becomes the
	// store's advertised earliest version, so height gates such as the EVM RPC
	// watermark admit pruned heights that the fallback can serve; reads below
	// it stay on the primary. When zero, the advertised earliest remains the
	// local prune horizon and height gates keep rejecting pruned heights.
	EarliestVersion int64
}

// FallbackStateStore routes pruned point reads to the historical reader.
// Iteration and writes stay on the primary state store.
type FallbackStateStore struct {
	primary        types.StateStore
	perKeyEarliest PerKeyEarliestVersioner
	reader         Reader
	cache          *lru.Cache[historicalReadCacheKey, historicalReadCacheValue]
	metrics        *fallbackMetrics
	coverageFloor  int64

	// backendLast caches the backend's last ingested version; it is refreshed
	// at most every backendVersionRecheckInterval when a read runs ahead of it.
	backendLast      atomic.Int64
	backendCheckedAt atomic.Int64
	backendMu        sync.Mutex
}

var _ types.StateStore = (*FallbackStateStore)(nil)

// NewFallbackStateStore takes ownership of primary and reader for Close.
func NewFallbackStateStore(primary types.StateStore, reader Reader, opts FallbackOptions) *FallbackStateStore {
	cache, err := lru.New[historicalReadCacheKey, historicalReadCacheValue](defaultHistoricalReadCacheEntries)
	if err != nil {
		panic(err)
	}
	perKeyEarliest, _ := primary.(PerKeyEarliestVersioner)
	return &FallbackStateStore{
		primary:        primary,
		perKeyEarliest: perKeyEarliest,
		reader:         reader,
		cache:          cache,
		metrics:        newFallbackMetrics(),
		coverageFloor:  opts.EarliestVersion,
	}
}

func (s *FallbackStateStore) shouldFallback(storeKey string, version int64) bool {
	if version < 0 || (s.coverageFloor > 0 && version < s.coverageFloor) {
		return false
	}
	earliest := s.earliestVersionFor(storeKey)
	return earliest > 0 && version < earliest
}

// errBackendBehind marks reads refused because the backend has not ingested
// the requested version yet, distinguishing lag from backend failures.
var errBackendBehind = errors.New("historical backend behind requested version")

// ensureBackendCoverage refuses fallback reads above the backend's last
// ingested version so consumer lag surfaces as an error instead of silently
// empty state (which would also poison the miss cache).
func (s *FallbackStateStore) ensureBackendCoverage(ctx context.Context, version int64) error {
	if version <= s.backendLast.Load() {
		return nil
	}
	s.backendMu.Lock()
	defer s.backendMu.Unlock()
	last := s.backendLast.Load()
	if version <= last {
		return nil
	}
	if time.Now().UnixNano()-s.backendCheckedAt.Load() >= backendVersionRecheckInterval.Nanoseconds() {
		refreshed, err := s.reader.LastVersion(ctx)
		if err != nil {
			return fmt.Errorf("check historical backend coverage: %w", err)
		}
		s.backendCheckedAt.Store(time.Now().UnixNano())
		if refreshed > last {
			last = refreshed
			s.backendLast.Store(refreshed)
		}
	}
	if version > last {
		return fmt.Errorf("%w: ingested up to version %d, requested %d", errBackendBehind, last, version)
	}
	return nil
}

func coverageOutcome(err error) string {
	if errors.Is(err, errBackendBehind) {
		return fallbackOutcomeBackendBehind
	}
	return fallbackOutcomeError
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
		s.metrics.recordRead("get", fallbackOutcomeCacheHit)
		if !found {
			return nil, nil
		}
		return value, nil
	}
	ctx, cancel := s.readContext()
	defer cancel()
	if err := s.ensureBackendCoverage(ctx, version); err != nil {
		s.metrics.recordRead("get", coverageOutcome(err))
		return nil, err
	}
	v, err := s.reader.Get(ctx, storeKey, key, version)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.metrics.recordRead("get", fallbackOutcomeBackendMiss)
			s.cacheMiss(cacheKey)
			return nil, nil
		}
		s.metrics.recordRead("get", fallbackOutcomeError)
		return nil, err
	}
	s.metrics.recordRead("get", fallbackOutcomeBackendHit)
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
		s.metrics.recordRead("has", fallbackOutcomeCacheHit)
		return found, nil
	}
	ctx, cancel := s.readContext()
	defer cancel()
	if err := s.ensureBackendCoverage(ctx, version); err != nil {
		s.metrics.recordRead("has", coverageOutcome(err))
		return false, err
	}
	found, err := s.reader.Has(ctx, storeKey, key, version)
	if err != nil {
		s.metrics.recordRead("has", fallbackOutcomeError)
		return false, err
	}
	if found {
		s.metrics.recordRead("has", fallbackOutcomeBackendHit)
		s.cacheHas(cacheKey)
	} else {
		s.metrics.recordRead("has", fallbackOutcomeBackendMiss)
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

// GetEarliestVersion advertises the earliest queryable version. With a
// configured coverage floor the fallback serves point reads below the local
// prune horizon, so height gates (e.g. the EVM RPC watermark) must see the
// floor rather than the local horizon or every historical query is rejected
// before it reaches the store. Iterators do not use the fallback; range
// queries between the floor and the local horizon see only local data.
func (s *FallbackStateStore) GetEarliestVersion() int64 {
	local := s.primary.GetEarliestVersion()
	if s.coverageFloor > 0 && local > 0 && s.coverageFloor < local {
		return s.coverageFloor
	}
	return local
}

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
