package util

import (
	"fmt"
	"sync"
	"testing"
)

func TestSyncMap_SetAndGet(t *testing.T) {
	m := NewSyncMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)

	v, ok := m.Get("a")
	if !ok || v != 1 {
		t.Fatalf("expected (1, true), got (%d, %v)", v, ok)
	}

	v, ok = m.Get("b")
	if !ok || v != 2 {
		t.Fatalf("expected (2, true), got (%d, %v)", v, ok)
	}

	_, ok = m.Get("missing")
	if ok {
		t.Fatal("expected missing key to return false")
	}
}

func TestSyncMap_PutBatch(t *testing.T) {
	m := NewSyncMap[string, int]()
	m.Set("pre", 0)

	batch := map[string]int{"x": 10, "y": 20, "z": 30}
	m.PutBatch(batch)

	if m.Len() != 4 {
		t.Fatalf("expected len 4, got %d", m.Len())
	}
	for k, want := range batch {
		v, ok := m.Get(k)
		if !ok || v != want {
			t.Fatalf("key %q: expected (%d, true), got (%d, %v)", k, want, v, ok)
		}
	}

	// Pre-existing key should still be there.
	v, ok := m.Get("pre")
	if !ok || v != 0 {
		t.Fatalf("pre-existing key: expected (0, true), got (%d, %v)", v, ok)
	}
}

func TestSyncMap_PutBatch_Overwrite(t *testing.T) {
	m := NewSyncMap[string, int]()
	m.Set("a", 1)
	m.PutBatch(map[string]int{"a": 99})

	v, ok := m.Get("a")
	if !ok || v != 99 {
		t.Fatalf("expected overwritten value 99, got %d", v)
	}
}

func TestSyncMap_Delete(t *testing.T) {
	m := NewSyncMap[string, int]()
	m.Set("a", 1)
	m.Delete("a")

	_, ok := m.Get("a")
	if ok {
		t.Fatal("expected key to be deleted")
	}

	// Deleting a non-existent key should be a no-op.
	m.Delete("nonexistent")
}

func TestSyncMap_DeleteBatch(t *testing.T) {
	m := NewSyncMap[string, int]()
	m.PutBatch(map[string]int{"a": 1, "b": 2, "c": 3, "d": 4})

	m.DeleteBatch([]string{"a", "c", "nonexistent"})

	if m.Len() != 2 {
		t.Fatalf("expected len 2 after delete batch, got %d", m.Len())
	}
	if _, ok := m.Get("a"); ok {
		t.Fatal("expected 'a' to be deleted")
	}
	if _, ok := m.Get("c"); ok {
		t.Fatal("expected 'c' to be deleted")
	}
	if v, ok := m.Get("b"); !ok || v != 2 {
		t.Fatalf("expected 'b' to survive, got (%d, %v)", v, ok)
	}
	if v, ok := m.Get("d"); !ok || v != 4 {
		t.Fatalf("expected 'd' to survive, got (%d, %v)", v, ok)
	}
}

func TestSyncMap_Len(t *testing.T) {
	m := NewSyncMap[int, string]()
	if m.Len() != 0 {
		t.Fatalf("expected empty map, got len %d", m.Len())
	}
	m.Set(1, "one")
	m.Set(2, "two")
	if m.Len() != 2 {
		t.Fatalf("expected len 2, got %d", m.Len())
	}
	m.Delete(1)
	if m.Len() != 1 {
		t.Fatalf("expected len 1 after delete, got %d", m.Len())
	}
}

func TestSyncMap_ConcurrentReadWrite(t *testing.T) {
	m := NewSyncMap[int, int]()
	const writers = 4
	const readers = 8
	const opsPerGoroutine = 1000

	var wg sync.WaitGroup

	// Writers store key=goroutineID*opsPerGoroutine+i, value=i
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range opsPerGoroutine {
				m.Set(w*opsPerGoroutine+i, i)
			}
		}()
	}

	// Readers continuously load random keys.
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range opsPerGoroutine {
				m.Get(i)
			}
		}()
	}

	wg.Wait()

	if m.Len() != writers*opsPerGoroutine {
		t.Fatalf("expected %d entries, got %d", writers*opsPerGoroutine, m.Len())
	}
}

func TestSyncMap_ConcurrentBatchWriteAndDelete(t *testing.T) {
	m := NewSyncMap[string, int]()
	const batchSize = 100
	const batches = 10

	var wg sync.WaitGroup

	// Multiple writer goroutines insert disjoint key ranges concurrently.
	for b := range batches {
		wg.Add(1)
		go func() {
			defer wg.Done()
			batch := make(map[string]int, batchSize)
			for i := range batchSize {
				batch[fmt.Sprintf("k-%d-%d", b, i)] = b*batchSize + i
			}
			m.PutBatch(batch)
		}()
	}
	wg.Wait()

	// All keys should be present.
	if m.Len() != batches*batchSize {
		t.Fatalf("expected %d entries, got %d", batches*batchSize, m.Len())
	}

	// Now delete batch 0 while concurrently reading batches 1-9.
	deleteKeys := make([]string, batchSize)
	for i := range batchSize {
		deleteKeys[i] = fmt.Sprintf("k-0-%d", i)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		m.DeleteBatch(deleteKeys)
	}()

	for b := 1; b < batches; b++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range batchSize {
				key := fmt.Sprintf("k-%d-%d", b, i)
				v, ok := m.Get(key)
				if !ok {
					t.Errorf("expected key %q to exist during concurrent read", key)
					return
				}
				if v != b*batchSize+i {
					t.Errorf("key %q: expected %d, got %d", key, b*batchSize+i, v)
					return
				}
			}
		}()
	}
	wg.Wait()

	// Batch 0 keys should be deleted; batches 1-9 should remain.
	for i := range batchSize {
		key := fmt.Sprintf("k-0-%d", i)
		if _, ok := m.Get(key); ok {
			t.Fatalf("expected key %q to be deleted", key)
		}
	}
	if m.Len() != (batches-1)*batchSize {
		t.Fatalf("expected %d entries after delete, got %d", (batches-1)*batchSize, m.Len())
	}
}
