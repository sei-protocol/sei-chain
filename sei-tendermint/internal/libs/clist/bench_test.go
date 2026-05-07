package clist

import "testing"

func BenchmarkPushBack(b *testing.B) {
	lst := New[int]()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lst.PushBack(i)
	}
}
