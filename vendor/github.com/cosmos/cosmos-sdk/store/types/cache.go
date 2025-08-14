package types

import (
	"sync"

	"github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/telemetry"
)

const DefaultCacheSizeLimit = 4000000 // TODO: revert back to 1000000 after paritioning r/w caches

// If value is nil but deleted is false, it means the parent doesn't have the
// key.  (No need to delete upon Write())
type CValue struct {
	value []byte
	dirty bool
}

func NewCValue(value []byte, dirty bool) *CValue {
	return &CValue{
		value: value,
		dirty: dirty,
	}
}

func (v *CValue) Value() []byte {
	return v.value
}

func (v *CValue) Dirty() bool {
	return v.dirty
}

type CacheBackend interface {
	Get(string) (*CValue, bool)
	Set(string, *CValue)
	Len() int
	Delete(string)
	Range(func(string, *CValue) bool)
}

// This struct is solely for the purpose of preventing the process from crashing because of
// OOM. It is not intended for usage at limit during normal operation. The node operator
// should minimize the time of running at cache limit by switching to a machine with larger
// RAM and bump up cache limit in app config, once the old limit is seen to be reached.
type BoundedCache struct {
	CacheBackend
	limit int

	mu         *sync.Mutex
	metricName []string
}

func NewBoundedCache(backend CacheBackend, limit int) *BoundedCache {
	if limit == 0 {
		panic("cache limit must be at least 1")
	}
	return &BoundedCache{
		CacheBackend: backend,
		limit:        limit,
		mu:           &sync.Mutex{},
		// cosmos_bounded_cache
		metricName: []string{"cosmos", "bounded", "cache"},
	}
}

func (c *BoundedCache) emitCacheSizeMetric() {
	telemetry.SetGaugeWithLabels(
		c.metricName,
		float32(c.Len()),
		[]metrics.Label{telemetry.NewLabel("type", "bounded_cache_size")},
	)
	telemetry.SetGaugeWithLabels(
		c.metricName,
		float32(c.limit),
		[]metrics.Label{telemetry.NewLabel("type", "bounded_cache_limit")},
	)
}

func (c *BoundedCache) emitKeysEvictedMetrics(keysToEvict int) {
	telemetry.SetGaugeWithLabels(
		c.metricName,
		float32(keysToEvict),
		[]metrics.Label{telemetry.NewLabel("type", "keys_evicted")},
	)
}

func (c *BoundedCache) Set(key string, val *CValue) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// defer c.emitCacheSizeMetric()

	if c.Len() >= c.limit {
		numEntries := c.Len()
		keysToEvict := []string{}
		c.CacheBackend.Range(func(key string, val *CValue) bool {
			if val.dirty {
				return true
			}
			keysToEvict = append(keysToEvict, key)
			numEntries--
			return numEntries >= c.limit
		})
		for _, key := range keysToEvict {
			c.CacheBackend.Delete(key)
		}
		c.emitKeysEvictedMetrics(len(keysToEvict))
	}
	c.CacheBackend.Set(key, val)
}

func (c *BoundedCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// defer c.emitCacheSizeMetric()

	c.CacheBackend.Delete(key)
}

func (c *BoundedCache) DeleteAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	// defer c.emitCacheSizeMetric()

	c.CacheBackend.Range(func(key string, _ *CValue) bool {
		c.CacheBackend.Delete(key)
		return true
	})
}

func (c *BoundedCache) Range(f func(string, *CValue) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.CacheBackend.Range(f)
}
