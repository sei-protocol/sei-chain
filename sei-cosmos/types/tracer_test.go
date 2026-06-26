package types

import (
	"testing"
	"time"
)

// retainedBytes sums the key/value payload the tracer holds until the trace
// response is built.
func retainedBytes(st *StoreTracer) int {
	total := 0
	for _, mt := range st.Modules {
		for _, a := range mt.Accesses {
			total += len(a.Key) + len(a.Value)
		}
		for _, it := range mt.Iterators {
			total += len(it.Start) + len(it.End)
			for _, k := range it.Keys {
				total += len(k)
			}
		}
	}
	return total
}

// TestStoreTracerBoundsIteratorScanMemory reproduces the blowup: mt.Accesses
// is uncapped, so a large iterator scan clones one key/value per surfaced key
// and retained memory grows linearly with scan size. The 8 MiB budget matches
// the file's "a few MB per module" intent.
func TestStoreTracerBoundsIteratorScanMemory(t *testing.T) {
	const (
		module    = "evm"
		numKeys   = 100_000 // a large-but-plausible scan over a big store
		valueSize = 512     // bytes per surfaced value

		maxRetainedBytes = 8 << 20 // 8 MiB
	)

	st := NewStoreTracer()
	iterID := st.StartIterator([]byte("start"), []byte("end"), true, module, 0)

	value := make([]byte, valueSize)
	for i := range numKeys {
		key := []byte{byte(i), byte(i >> 8), byte(i >> 16)}
		st.RecordIteratorNext(iterID, module, time.Microsecond)
		st.RecordIteratorValue(iterID, key, value, module)
	}

	got := retainedBytes(st)
	if got > maxRetainedBytes {
		t.Fatalf("StoreTracer retained %d bytes after scanning %d keys; want <= %d.\n"+
			"mt.Accesses is unbounded: each IteratorValue clones key+value into the "+
			"access log regardless of the %d-key iterator cap, so memory grows linearly "+
			"with the scan size.",
			got, numKeys, maxRetainedBytes, maxStoreTraceIteratorKeys)
	}
}
