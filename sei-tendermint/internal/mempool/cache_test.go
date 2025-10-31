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
		cache := NewLRUTxCache(10, 0)
		assert.NotNil(t, cache)
		assert.Equal(t, 10, cache.size)
		assert.NotNil(t, cache.cacheMap)
		assert.NotNil(t, cache.list)
	})

	t.Run("Push_NewTransaction", func(t *testing.T) {
		cache := NewLRUTxCache(3, 0)
		tx := types.Tx("test1").Key()

		// First push should return true (newly added)
		result := cache.Push(tx)
		assert.True(t, result)
		assert.Equal(t, 1, cache.Size())
	})

	t.Run("Push_DuplicateTransaction", func(t *testing.T) {
		cache := NewLRUTxCache(3, 0)
		tx := types.Tx("test1").Key()

		// First push
		result := cache.Push(tx)
		assert.True(t, result)

		// Second push of same transaction should return false
		result = cache.Push(tx)
		assert.False(t, result)
		assert.Equal(t, 1, cache.Size())
	})

	t.Run("Push_CacheFull", func(t *testing.T) {
		cache := NewLRUTxCache(2, 0)

		// Add two transactions
		tx1 := types.Tx("test1").Key()
		tx2 := types.Tx("test2").Key()

		cache.Push(tx1)
		cache.Push(tx2)
		assert.Equal(t, 2, cache.Size())

		// Add third transaction, should evict the first one (LRU)
		tx3 := types.Tx("test3").Key()
		cache.Push(tx3)
		assert.Equal(t, 2, cache.Size())

		// First transaction should be evicted, so pushing it again should return true
		assert.True(t, cache.Push(tx1)) // Should return true as it's newly added again
	})

	t.Run("Remove_ExistingTransaction", func(t *testing.T) {
		cache := NewLRUTxCache(3, 0)
		tx := types.Tx("test1").Key()

		cache.Push(tx)
		assert.Equal(t, 1, cache.Size())

		cache.Remove(tx)
		assert.Equal(t, 0, cache.Size())
	})

	t.Run("Remove_NonExistentTransaction", func(t *testing.T) {
		cache := NewLRUTxCache(3, 0)
		tx := types.Tx("test1").Key()

		// Remove non-existent transaction should not panic
		cache.Remove(tx)
		assert.Equal(t, 0, cache.Size())
	})

	t.Run("Reset", func(t *testing.T) {
		cache := NewLRUTxCache(3, 0)

		// Add some transactions
		cache.Push(types.Tx("test1").Key())
		cache.Push(types.Tx("test2").Key())
		assert.Equal(t, 2, cache.Size())

		// Reset should clear everything
		cache.Reset()
		assert.Equal(t, 0, cache.Size())
	})

	t.Run("Size", func(t *testing.T) {
		cache := NewLRUTxCache(3, 0)
		assert.Equal(t, 0, cache.Size())

		cache.Push(types.Tx("test1").Key())
		assert.Equal(t, 1, cache.Size())

		cache.Push(types.Tx("test2").Key())
		assert.Equal(t, 2, cache.Size())
	})
}

func TestNopTxCache(t *testing.T) {
	cache := NopTxCache{}

	t.Run("Reset", func(t *testing.T) {
		// Should not panic
		cache.Reset()
	})

	t.Run("Push", func(t *testing.T) {
		tx := types.Tx("test").Key()
		result := cache.Push(tx)
		assert.True(t, result)
	})

	t.Run("Remove", func(t *testing.T) {
		tx := types.Tx("test").Key()
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
		cache := NewDuplicateTxCache(100, 100*time.Millisecond, 0)
		assert.NotNil(t, cache)
		assert.NotNil(t, cache.cache)
	})

	t.Run("NewDuplicateTxCache_NoExpiration", func(t *testing.T) {
		cache := NewDuplicateTxCache(100, 0, 0)
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

	t.Run("Increment_CacheFull_NoEffect", func(t *testing.T) {
		// Create a cache with maxSize=2
		cache := NewDuplicateTxCache(2, 10*time.Second, 0)

		// Add items up to the maxSize using Set (which doesn't check maxSize)
		txKey1 := createTestTxKey("key1")
		txKey2 := createTestTxKey("key2")
		txKey3 := createTestTxKey("key3") // This will be the key we try to increment when cache is at max size

		// Add two items to reach maxSize
		cache.Set(txKey1, 1)
		cache.Set(txKey2, 1)

		// Verify cache is at max size
		assert.Equal(t, 2, cache.cache.ItemCount())
		assert.Equal(t, cache.cache.ItemCount(), cache.maxSize)

		// Try to increment a new key when cache is at max size
		// The go-cache.Increment() will fail because the key doesn't exist,
		// and then our code will check if cache.ItemCount() < maxSize
		// Since cache.ItemCount() (2) is NOT < maxSize (2), it should NOT add the key
		cache.Increment(txKey3)

		// Verify the new key was not added
		counter, found := cache.Get(txKey3)
		assert.False(t, found)
		assert.Equal(t, 0, counter)

		// Verify cache size is still the same (the Increment should not have added a new item)
		assert.Equal(t, 2, cache.cache.ItemCount())

		// Verify existing keys are still there
		counter1, found1 := cache.Get(txKey1)
		assert.True(t, found1)
		assert.Equal(t, 1, counter1)

		counter2, found2 := cache.Get(txKey2)
		assert.True(t, found2)
		assert.Equal(t, 1, counter2)
	})

	t.Run("Increment_CacheNotFull_ShouldWork", func(t *testing.T) {
		// Create a cache with size 3, but only add 1 item
		cache := NewDuplicateTxCache(3, 10*time.Second, 0)

		txKey1 := createTestTxKey("key1")
		txKey2 := createTestTxKey("key2")

		// Add one item
		cache.Set(txKey1, 1)
		assert.Equal(t, 1, cache.cache.ItemCount())

		// Increment a new key when cache is not full
		// This should work because cache.ItemCount() <= maxSize
		cache.Increment(txKey2)

		// Verify the new key was added
		counter, found := cache.Get(txKey2)
		assert.True(t, found)
		assert.Equal(t, 1, counter)

		// Verify cache size increased
		assert.Equal(t, 2, cache.cache.ItemCount())
	})

	t.Run("Increment_ExistingKey_CacheFull_ShouldWork", func(t *testing.T) {
		// Create a cache with size 2
		cache := NewDuplicateTxCache(2, 100*time.Millisecond, 0)

		txKey1 := createTestTxKey("key1")
		txKey2 := createTestTxKey("key2")

		// Fill the cache
		cache.Set(txKey1, 1)
		cache.Set(txKey2, 1)
		assert.Equal(t, 2, cache.cache.ItemCount())

		// Increment an existing key when cache is full
		// This should work because Increment() on existing keys doesn't add new items
		cache.Increment(txKey1)

		// Verify the existing key was incremented
		counter, found := cache.Get(txKey1)
		assert.True(t, found)
		assert.Equal(t, 2, counter)

		// Verify cache size is still the same
		assert.Equal(t, 2, cache.cache.ItemCount())
	})
}

func TestLRUTxCache_ConcurrentAccess(t *testing.T) {
	cache := NewLRUTxCache(100, 0)

	// Test concurrent access
	const numGoroutines = 10
	const operationsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < operationsPerGoroutine; j++ {
				tx := types.Tx(fmt.Sprintf("goroutine_%d_tx_%d", id, j)).Key()
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
		cache := NewLRUTxCache(0, 0)
		tx := types.Tx("test").Key()

		// Should handle zero size gracefully
		result := cache.Push(tx)
		assert.True(t, result)
		assert.Equal(t, 1, cache.Size())
	})

	t.Run("NegativeSizeCache", func(t *testing.T) {
		cache := NewLRUTxCache(-1, 0)
		tx := types.Tx("test").Key()

		// Should handle negative size gracefully
		result := cache.Push(tx)
		assert.True(t, result)
		assert.Equal(t, 1, cache.Size())
	})

	t.Run("NilTransaction", func(t *testing.T) {
		cache := NewLRUTxCache(10, 0)
		var tx types.TxKey

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
