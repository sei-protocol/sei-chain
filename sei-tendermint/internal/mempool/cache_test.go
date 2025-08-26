package mempool

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/tendermint/tendermint/types"
)

func TestLRUTxCache(t *testing.T) {
	t.Run("NewLRUTxCache", func(t *testing.T) {
		cache := NewLRUTxCache(10)
		assert.NotNil(t, cache)
		assert.Equal(t, 10, cache.size)
		assert.NotNil(t, cache.cacheMap)
		assert.NotNil(t, cache.list)
	})

	t.Run("Push_NewTransaction", func(t *testing.T) {
		cache := NewLRUTxCache(3)
		tx := types.Tx([]byte("test1"))

		// First push should return true (newly added)
		result := cache.Push(tx)
		assert.True(t, result)
		assert.Equal(t, 1, cache.Size())
	})

	t.Run("Push_DuplicateTransaction", func(t *testing.T) {
		cache := NewLRUTxCache(3)
		tx := types.Tx([]byte("test1"))

		// First push
		result := cache.Push(tx)
		assert.True(t, result)

		// Second push of same transaction should return false
		result = cache.Push(tx)
		assert.False(t, result)
		assert.Equal(t, 1, cache.Size())
	})

	t.Run("Push_CacheFull", func(t *testing.T) {
		cache := NewLRUTxCache(2)

		// Add two transactions
		tx1 := types.Tx([]byte("test1"))
		tx2 := types.Tx([]byte("test2"))

		cache.Push(tx1)
		cache.Push(tx2)
		assert.Equal(t, 2, cache.Size())

		// Add third transaction, should evict the first one (LRU)
		tx3 := types.Tx([]byte("test3"))
		cache.Push(tx3)
		assert.Equal(t, 2, cache.Size())

		// First transaction should be evicted, so pushing it again should return true
		assert.True(t, cache.Push(tx1)) // Should return true as it's newly added again
	})

	t.Run("Remove_ExistingTransaction", func(t *testing.T) {
		cache := NewLRUTxCache(3)
		tx := types.Tx([]byte("test1"))

		cache.Push(tx)
		assert.Equal(t, 1, cache.Size())

		cache.Remove(tx)
		assert.Equal(t, 0, cache.Size())
	})

	t.Run("Remove_NonExistentTransaction", func(t *testing.T) {
		cache := NewLRUTxCache(3)
		tx := types.Tx([]byte("test1"))

		// Remove non-existent transaction should not panic
		cache.Remove(tx)
		assert.Equal(t, 0, cache.Size())
	})

	t.Run("Reset", func(t *testing.T) {
		cache := NewLRUTxCache(3)

		// Add some transactions
		cache.Push(types.Tx([]byte("test1")))
		cache.Push(types.Tx([]byte("test2")))
		assert.Equal(t, 2, cache.Size())

		// Reset should clear everything
		cache.Reset()
		assert.Equal(t, 0, cache.Size())
	})

	t.Run("Size", func(t *testing.T) {
		cache := NewLRUTxCache(3)
		assert.Equal(t, 0, cache.Size())

		cache.Push(types.Tx([]byte("test1")))
		assert.Equal(t, 1, cache.Size())

		cache.Push(types.Tx([]byte("test2")))
		assert.Equal(t, 2, cache.Size())
	})

	t.Run("GetList", func(t *testing.T) {
		cache := NewLRUTxCache(3)
		list := cache.GetList()

		assert.NotNil(t, list)
		assert.Equal(t, 0, list.Len())

		// Add transaction and verify list is updated
		cache.Push(types.Tx([]byte("test1")))
		assert.Equal(t, 1, list.Len())
	})
}

func TestNopTxCache(t *testing.T) {
	cache := NopTxCache{}

	t.Run("Reset", func(t *testing.T) {
		// Should not panic
		cache.Reset()
	})

	t.Run("Push", func(t *testing.T) {
		tx := types.Tx([]byte("test"))
		result := cache.Push(tx)
		assert.True(t, result)
	})

	t.Run("Remove", func(t *testing.T) {
		tx := types.Tx([]byte("test"))
		// Should not panic
		cache.Remove(tx)
	})

	t.Run("Size", func(t *testing.T) {
		size := cache.Size()
		assert.Equal(t, 0, size)
	})
}

func TestDuplicateTxCache(t *testing.T) {
	t.Run("NewDuplicateTxCache_WithExpiration", func(t *testing.T) {
		cache := NewDuplicateTxCache(100, 100*time.Millisecond, 50*time.Millisecond)
		assert.NotNil(t, cache)
		assert.NotNil(t, cache.cache)
	})

	t.Run("NewDuplicateTxCache_NoExpiration", func(t *testing.T) {
		cache := NewDuplicateTxCache(100, 0, 50*time.Millisecond)
		assert.NotNil(t, cache)
		assert.NotNil(t, cache.cache)
	})

	t.Run("Set_And_Get", func(t *testing.T) {
		cache := NewDuplicateTxCache(100, 100*time.Millisecond, 0)
		txKey := createTestTxKey("test_key")

		// Set value
		cache.Set(txKey, 5)

		// Get value
		counter, found := cache.Get(txKey)
		assert.True(t, found)
		assert.Equal(t, 5, counter)
	})

	t.Run("Get_NonExistent", func(t *testing.T) {
		cache := NewDuplicateTxCache(100, 100*time.Millisecond, 0)
		txKey := createTestTxKey("non_existent")

		counter, found := cache.Get(txKey)
		assert.False(t, found)
		assert.Equal(t, 0, counter)
	})

	t.Run("Increment_NewKey", func(t *testing.T) {
		cache := NewDuplicateTxCache(100, 100*time.Millisecond, 0)
		txKey := createTestTxKey("new_key")

		// Increment non-existent key should start with 1
		cache.Increment(txKey)

		// Verify it was stored
		counter, found := cache.Get(txKey)
		assert.True(t, found)
		assert.Equal(t, 1, counter)
	})

	t.Run("Increment_ExistingKey", func(t *testing.T) {
		cache := NewDuplicateTxCache(100, 100*time.Millisecond, 0)
		txKey := createTestTxKey("existing_key")

		// Set initial value
		cache.Set(txKey, 3)

		// Increment existing key
		cache.Increment(txKey)

		// Verify it was updated
		counter, found := cache.Get(txKey)
		assert.True(t, found)
		assert.Equal(t, 4, counter)
	})

	t.Run("Reset", func(t *testing.T) {
		cache := NewDuplicateTxCache(100, 100*time.Millisecond, 0)
		txKey := createTestTxKey("test_key")

		// Add some data
		cache.Set(txKey, 5)
		counter, found := cache.Get(txKey)
		assert.True(t, found)
		assert.Equal(t, 5, counter)

		// Reset should clear everything
		cache.Reset()

		counter, found = cache.Get(txKey)
		assert.False(t, found)
		assert.Equal(t, 0, counter)
	})

	t.Run("Stop", func(t *testing.T) {
		cache := NewDuplicateTxCache(100, 100*time.Millisecond, 0)
		txKey := createTestTxKey("test_key")

		// Add some data
		cache.Set(txKey, 5)

		// Stop should clear the cache
		cache.Stop()

		counter, found := cache.Get(txKey)
		assert.False(t, found)
		assert.Equal(t, 0, counter)
	})

	t.Run("GetForMetrics", func(t *testing.T) {
		cache := NewDuplicateTxCache(100, 100*time.Millisecond, 0)

		// Add various transactions with different counts
		cache.Set(createTestTxKey("key1"), 1) // Non-duplicate
		cache.Set(createTestTxKey("key2"), 3) // Duplicate (count 3)
		cache.Set(createTestTxKey("key3"), 2) // Duplicate (count 2)
		cache.Set(createTestTxKey("key4"), 1) // Non-duplicate
		cache.Set(createTestTxKey("key5"), 4) // Duplicate (count 4)

		maxCount, totalCount, duplicateCount, nonDuplicateCount := cache.GetForMetrics()

		assert.Equal(t, 4, maxCount)          // Highest count
		assert.Equal(t, 6, totalCount)        // Sum of (count-1) for duplicates: (3-1)+(2-1)+(4-1) = 2+1+3 = 6
		assert.Equal(t, 3, duplicateCount)    // Number of keys with count > 1
		assert.Equal(t, 2, nonDuplicateCount) // Number of keys with count = 1
	})

	t.Run("GetForMetrics_EmptyCache", func(t *testing.T) {
		cache := NewDuplicateTxCache(100, 100*time.Millisecond, 0)

		maxCount, totalCount, duplicateCount, nonDuplicateCount := cache.GetForMetrics()

		assert.Equal(t, 0, maxCount)
		assert.Equal(t, 0, totalCount)
		assert.Equal(t, 0, duplicateCount)
		assert.Equal(t, 0, nonDuplicateCount)
	})
}

func TestNopTxCacheWithTTL(t *testing.T) {
	cache := NopTxCacheWithTTL{}

	t.Run("Set", func(t *testing.T) {
		txKey := createTestTxKey("test")
		// Should not panic
		cache.Set(txKey, 5)
	})

	t.Run("Get", func(t *testing.T) {
		txKey := createTestTxKey("test")
		counter, found := cache.Get(txKey)
		assert.False(t, found)
		assert.Equal(t, 0, counter)
	})

	t.Run("Increment", func(t *testing.T) {
		txKey := createTestTxKey("test")
		cache.Increment(txKey)
		count, found := cache.Get(txKey)
		// NOP cache should always return 0 and false, regardless of operations
		assert.Equal(t, 0, count)
		assert.False(t, found)
	})

	t.Run("Reset", func(t *testing.T) {
		// Should not panic
		cache.Reset()
	})

	t.Run("Stop", func(t *testing.T) {
		// Should not panic
		cache.Stop()
	})

	t.Run("GetForMetrics", func(t *testing.T) {
		maxCount, totalCount, duplicateCount, nonDuplicateCount := cache.GetForMetrics()
		assert.Equal(t, 0, maxCount)
		assert.Equal(t, 0, totalCount)
		assert.Equal(t, 0, duplicateCount)
		assert.Equal(t, 0, nonDuplicateCount)
	})
}

func TestTxKeyToString(t *testing.T) {
	t.Run("EmptyKey", func(t *testing.T) {
		var txKey types.TxKey
		result := txKeyToString(txKey)
		assert.Equal(t, "0000000000000000000000000000000000000000000000000000000000000000", result)
	})

	t.Run("SimpleKey", func(t *testing.T) {
		txKey := createTestTxKey("hello")
		result := txKeyToString(txKey)
		// The result will be the hex representation of the key
		assert.NotEmpty(t, result)
		assert.Len(t, result, 64) // 32 bytes = 64 hex chars
	})

	t.Run("BinaryKey", func(t *testing.T) {
		var txKey types.TxKey
		// Set some specific bytes
		txKey[0] = 0x00
		txKey[1] = 0x01
		txKey[2] = 0x02
		txKey[31] = 0xFF

		result := txKeyToString(txKey)
		assert.NotEmpty(t, result)
		assert.Len(t, result, 64) // 32 bytes = 64 hex chars
	})
}

func TestLRUTxCache_ConcurrentAccess(t *testing.T) {
	cache := NewLRUTxCache(100)

	// Test concurrent access
	const numGoroutines = 10
	const operationsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < operationsPerGoroutine; j++ {
				tx := types.Tx([]byte(fmt.Sprintf("goroutine_%d_tx_%d", id, j)))
				cache.Push(tx)

				if j%10 == 0 {
					cache.Size() // Read operation
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify final state is reasonable
	size := cache.Size()
	assert.True(t, size > 0)
	assert.True(t, size <= 100) // Should not exceed cache size
}

func TestDuplicateTxCache_ConcurrentAccess(t *testing.T) {
	cache := NewDuplicateTxCache(100, 100*time.Millisecond, 0)

	// Test concurrent access
	const numGoroutines = 10
	const operationsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < operationsPerGoroutine; j++ {
				txKey := createTestTxKey(fmt.Sprintf("goroutine_%d_key_%d", id, j))

				// Mix of operations
				switch j % 3 {
				case 0:
					cache.Set(txKey, j+1)
				case 1:
					cache.Get(txKey)
				case 2:
					cache.Increment(txKey)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify final state is reasonable
	maxCount, totalCount, duplicateCount, nonDuplicateCount := cache.GetForMetrics()
	assert.True(t, maxCount >= 0)
	assert.True(t, totalCount >= 0)
	assert.True(t, duplicateCount >= 0)
	assert.True(t, nonDuplicateCount >= 0)
}

func TestLRUTxCache_EdgeCases(t *testing.T) {
	t.Run("ZeroSizeCache", func(t *testing.T) {
		cache := NewLRUTxCache(0)
		tx := types.Tx([]byte("test"))

		// Should handle zero size gracefully
		result := cache.Push(tx)
		assert.True(t, result)
		assert.Equal(t, 1, cache.Size())
	})

	t.Run("NegativeSizeCache", func(t *testing.T) {
		cache := NewLRUTxCache(-1)
		tx := types.Tx([]byte("test"))

		// Should handle negative size gracefully
		result := cache.Push(tx)
		assert.True(t, result)
		assert.Equal(t, 1, cache.Size())
	})

	t.Run("NilTransaction", func(t *testing.T) {
		cache := NewLRUTxCache(10)
		var tx types.Tx

		// Should handle nil transaction gracefully
		result := cache.Push(tx)
		assert.True(t, result)
		assert.Equal(t, 1, cache.Size())
	})
}

func TestDuplicateTxCache_EdgeCases(t *testing.T) {
	t.Run("ZeroExpiration", func(t *testing.T) {
		cache := NewDuplicateTxCache(100, 0, 0)
		txKey := createTestTxKey("test")

		// Should work with zero expiration
		cache.Set(txKey, 5)
		counter, found := cache.Get(txKey)
		assert.True(t, found)
		assert.Equal(t, 5, counter)
	})

	t.Run("EmptyTxKey", func(t *testing.T) {
		cache := NewDuplicateTxCache(100, 100*time.Millisecond, 0)
		var txKey types.TxKey

		// Should handle empty key gracefully
		cache.Set(txKey, 5)
		counter, found := cache.Get(txKey)
		assert.True(t, found)
		assert.Equal(t, 5, counter)
	})

	t.Run("VeryLargeExpiration", func(t *testing.T) {
		cache := NewDuplicateTxCache(100, 24*365*time.Hour, 0) // 1 year
		txKey := createTestTxKey("test")

		// Should work with very large expiration
		cache.Set(txKey, 5)
		counter, found := cache.Get(txKey)
		assert.True(t, found)
		assert.Equal(t, 5, counter)
	})
}

func TestCache_InterfaceCompliance(t *testing.T) {
	// Test that all implementations properly implement their interfaces

	t.Run("LRUTxCache_Implements_TxCache", func(t *testing.T) {
		var _ TxCache = (*LRUTxCache)(nil)
	})

	t.Run("NopTxCache_Implements_TxCache", func(t *testing.T) {
		var _ TxCache = (*NopTxCache)(nil)
	})

	t.Run("DuplicateTxCache_Implements_TxCacheWithTTL", func(t *testing.T) {
		var _ TxCacheWithTTL = (*DuplicateTxCache)(nil)
	})

	t.Run("NopTxCacheWithTTL_Implements_TxCacheWithTTL", func(t *testing.T) {
		var _ TxCacheWithTTL = (*NopTxCacheWithTTL)(nil)
	})
}

// createTestTxKey creates a test TxKey from a string by hashing it
func createTestTxKey(input string) types.TxKey {
	// Create a simple hash-like key for testing
	var key types.TxKey
	hash := []byte(input)

	// Copy hash bytes to key, padding with zeros if needed
	for i := 0; i < len(key) && i < len(hash); i++ {
		key[i] = hash[i]
	}

	return key
}
