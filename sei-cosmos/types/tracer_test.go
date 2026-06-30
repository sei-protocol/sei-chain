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

// TestStoreTracerStatsAccurateWhenTruncated drives Get past
// maxStoreTraceModuleBytes and asserts that per-op stats (Count/TotalNanos) still
// count every access once the log is Truncated, while Reads keeps only the
// retained prefix. These stats feed historicalLookupNanos (evmrpc/trace_profile.go).
func TestStoreTracerStatsAccurateWhenTruncated(t *testing.T) {
	const (
		module    = "evm"
		numGets   = 5_000            // each retained Get costs valueSize+keyLen+overhead;
		valueSize = 4096             // ~4 KiB per access truncates well before all 5k retain
		perGet    = time.Microsecond // known per-access duration to verify TotalNanos
	)

	st := NewStoreTracer()
	value := make([]byte, valueSize)
	for i := range numGets {
		// Distinct keys so each retained Get is a distinct entry in Reads.
		key := []byte{byte(i), byte(i >> 8), byte(i >> 16)}
		st.Get(key, value, module, perGet)
	}

	mt, ok := st.Modules[module]
	if !ok {
		t.Fatalf("module %q missing from tracer", module)
	}
	if !mt.Truncated {
		t.Fatalf("module not Truncated after %d Gets of %d-byte values; cap %d should have been exceeded",
			numGets, valueSize, maxStoreTraceModuleBytes)
	}
	if len(mt.Accesses) >= numGets {
		t.Fatalf("retained %d accesses; truncation should drop some of the %d Gets",
			len(mt.Accesses), numGets)
	}

	// Internal running stats must count every access, truncated or not.
	if got := mt.stats[Get]; got.Count != numGets || got.TotalNanos != int64(numGets)*perGet.Nanoseconds() {
		t.Fatalf("module stats under truncation = {Count:%d TotalNanos:%d}; want {Count:%d TotalNanos:%d}",
			got.Count, got.TotalNanos, numGets, int64(numGets)*perGet.Nanoseconds())
	}

	// The wire dump (what historicalLookupNanos reads) must agree, and Reads
	// must reflect only the retained prefix.
	d := st.Dump()
	mtd := d.Modules[module]
	if !mtd.Truncated {
		t.Fatal("dump module not flagged Truncated")
	}
	if got := mtd.Stats[Get.String()]; got.Count != numGets || got.TotalNanos != int64(numGets)*perGet.Nanoseconds() {
		t.Fatalf("dump module stats = {Count:%d TotalNanos:%d}; want {Count:%d TotalNanos:%d}",
			got.Count, got.TotalNanos, numGets, int64(numGets)*perGet.Nanoseconds())
	}
	if top := d.Stats[Get.String()]; top.Count != numGets || top.TotalNanos != int64(numGets)*perGet.Nanoseconds() {
		t.Fatalf("dump top-level stats = {Count:%d TotalNanos:%d}; want {Count:%d TotalNanos:%d}",
			top.Count, top.TotalNanos, numGets, int64(numGets)*perGet.Nanoseconds())
	}
	if len(mtd.Reads) >= numGets {
		t.Fatalf("dump retained %d reads; truncation should bound this below %d", len(mtd.Reads), numGets)
	}
}
