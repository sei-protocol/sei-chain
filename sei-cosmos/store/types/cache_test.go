package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type mapCacheBackend struct {
	m map[string]*CValue
}

func (b mapCacheBackend) Get(key string) (val *CValue, ok bool) {
	val, ok = b.m[key]
	return
}

func (b mapCacheBackend) Set(key string, val *CValue) {
	b.m[key] = val
}

func (b mapCacheBackend) Len() int {
	return len(b.m)
}

func (b mapCacheBackend) Delete(key string) {
	delete(b.m, key)
}

func (b mapCacheBackend) Range(f func(string, *CValue) bool) {
	for k, v := range b.m {
		if !f(k, v) {
			break
		}
	}
}

func TestCacheLimit(t *testing.T) {
	cache := NewBoundedCache(mapCacheBackend{make(map[string]*CValue)}, 2)
	require.Equal(t, 0, cache.Len())
	cache.Set("abc", &CValue{value: []byte("123")})
	require.Equal(t, 1, cache.Len())
	v, ok := cache.Get("abc")
	require.True(t, ok)
	require.Equal(t, "123", string(v.value))
	cache.Set("def", &CValue{value: []byte("456")})
	require.Equal(t, 2, cache.Len())
	v, ok = cache.Get("abc")
	require.True(t, ok)
	require.Equal(t, "123", string(v.value))
	v, ok = cache.Get("def")
	require.True(t, ok)
	require.Equal(t, "456", string(v.value))
	cache.Set("ghi", &CValue{value: []byte("789")})
	require.Equal(t, 2, cache.Len())
	v, ok = cache.Get("ghi")
	require.True(t, ok)
	require.Equal(t, "789", string(v.value))
	// only one of abc and def should still exist in the cache. We don't care which one
	if v, ok := cache.Get("abc"); ok {
		require.Equal(t, "123", string(v.value))
		_, ok = cache.Get("def")
		require.False(t, ok)
	} else {
		v, ok = cache.Get("def")
		require.True(t, ok)
		require.Equal(t, "456", string(v.value))
	}

	cache.Delete("ghi")
	require.Equal(t, 1, cache.Len())
}
