//go:build !race

package mvcc

import "testing"

func TestBatchSetAllocs(t *testing.T) {
	b, err := NewBatch(nil, 1, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	b.grow(8)

	key := []byte("key")
	val := []byte("value")
	allocs := testing.AllocsPerRun(1000, func() {
		b.Reset()
		if err := b.Set("store", key, val); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 2 {
		t.Fatalf("Set allocated %.1f times per call, want <= 2 (cloned key + cloned value)", allocs)
	}
}
