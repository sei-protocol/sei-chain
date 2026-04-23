package clist

import "testing"

func BenchmarkDetaching(b *testing.B) {
	lst := New[int]()
	for i := 0; i < b.N+1; i++ {
		lst.PushBack(i)
	}
	start := lst.Front()
	nxt := start.Next()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start.removed = true
		start.detachNext()
		start.DetachPrev()
		tmp := nxt
		nxt = nxt.Next()
		start = tmp
	}
}

// This is used to benchmark the time of RMutex.
func BenchmarkRemoved(b *testing.B) {
	lst := New[int]()
	for i := 0; i < b.N+1; i++ {
		lst.PushBack(i)
	}
	start := lst.Front()
	nxt := start.Next()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start.Removed()
		tmp := nxt
		nxt = nxt.Next()
		start = tmp
	}
}

func BenchmarkPushBack(b *testing.B) {
	lst := New[int]()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lst.PushBack(i)
	}
}
