package types

import "sync"

const DefaultCacheSizeLimit = 1000000

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

	mu *sync.Mutex
}

func NewBoundedCache(backend CacheBackend, limit int) *BoundedCache {
	if limit == 0 {
		panic("cache limit must be at least 1")
	}
	return &BoundedCache{
		CacheBackend: backend,
		limit:        limit,
		mu:           &sync.Mutex{},
	}
}

func (c *BoundedCache) Set(key string, val *CValue) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Len() >= c.limit {
		len := c.Len()
		keysToEvict := []string{}
		c.CacheBackend.Range(func(key string, _ *CValue) bool {
			keysToEvict = append(keysToEvict, key)
			len--
			return len >= c.limit
		})
		for _, key := range keysToEvict {
			c.CacheBackend.Delete(key)
		}
	}
	c.CacheBackend.Set(key, val)
}

func (c *BoundedCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.CacheBackend.Delete(key)
}

func (c *BoundedCache) DeleteAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

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
