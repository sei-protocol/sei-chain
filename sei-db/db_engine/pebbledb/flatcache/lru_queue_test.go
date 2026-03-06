package flatcache

import (
	"bytes"
	"testing"
)

func TestLRUQueueTracksSizeCountAndOrder(t *testing.T) {
	lru := NewLRUQueue()

	lru.Push([]byte("a"), 3)
	lru.Push([]byte("b"), 5)
	lru.Push([]byte("c"), 7)

	if got := lru.GetCount(); got != 3 {
		t.Fatalf("GetCount() = %d, want 3", got)
	}

	if got := lru.GetTotalSize(); got != 15 {
		t.Fatalf("GetTotalSize() = %d, want 15", got)
	}

	lru.Touch([]byte("a"))

	if got := lru.PopLeastRecentlyUsed(); !bytes.Equal(got, []byte("b")) {
		t.Fatalf("first pop = %q, want %q", got, []byte("b"))
	}

	if got := lru.PopLeastRecentlyUsed(); !bytes.Equal(got, []byte("c")) {
		t.Fatalf("second pop = %q, want %q", got, []byte("c"))
	}

	if got := lru.PopLeastRecentlyUsed(); !bytes.Equal(got, []byte("a")) {
		t.Fatalf("third pop = %q, want %q", got, []byte("a"))
	}

	if got := lru.GetCount(); got != 0 {
		t.Fatalf("GetCount() after pops = %d, want 0", got)
	}

	if got := lru.GetTotalSize(); got != 0 {
		t.Fatalf("GetTotalSize() after pops = %d, want 0", got)
	}
}

func TestLRUQueuePushUpdatesExistingEntry(t *testing.T) {
	lru := NewLRUQueue()

	lru.Push([]byte("a"), 3)
	lru.Push([]byte("b"), 5)
	lru.Push([]byte("a"), 11)

	if got := lru.GetCount(); got != 2 {
		t.Fatalf("GetCount() = %d, want 2", got)
	}

	if got := lru.GetTotalSize(); got != 16 {
		t.Fatalf("GetTotalSize() = %d, want 16", got)
	}

	if got := lru.PopLeastRecentlyUsed(); !bytes.Equal(got, []byte("b")) {
		t.Fatalf("first pop = %q, want %q", got, []byte("b"))
	}

	if got := lru.PopLeastRecentlyUsed(); !bytes.Equal(got, []byte("a")) {
		t.Fatalf("second pop = %q, want %q", got, []byte("a"))
	}
}

func TestLRUQueueCopiesInsertedKey(t *testing.T) {
	lru := NewLRUQueue()

	key := []byte("a")
	lru.Push(key, 1)
	key[0] = 'z'

	if got := lru.PopLeastRecentlyUsed(); !bytes.Equal(got, []byte("a")) {
		t.Fatalf("pop after mutating caller key = %q, want %q", got, []byte("a"))
	}
}

// TODO expand these tests
