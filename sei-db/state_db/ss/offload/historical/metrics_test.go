package historical

import (
	"context"
	"testing"
	"time"
)

func TestBigtableRowSize(t *testing.T) {
	row := BigtableRow{
		Key: "abc", // 3
		Cells: []BigtableCell{
			{Qualifier: "value", Value: []byte("hello")}, // 5 + 5
			{Qualifier: "deleted", Value: []byte{0x1}},   // 7 + 1
		},
	}
	if got, want := bigtableRowSize(row), int64(3+5+5+7+1); got != want {
		t.Fatalf("bigtableRowSize() = %d, want %d", got, want)
	}
	if got := bigtableRowSize(BigtableRow{}); got != 0 {
		t.Fatalf("bigtableRowSize(empty) = %d, want 0", got)
	}
}

func TestBigtableMutationSize(t *testing.T) {
	row := BigtableRowMutation{
		RowKey: "row1", // 4
		SetCells: []BigtableSetCell{
			{Qualifier: "value", Value: []byte("data")}, // 5 + 4
		},
	}
	if got, want := bigtableMutationSize(row), int64(4+5+4); got != want {
		t.Fatalf("bigtableMutationSize() = %d, want %d", got, want)
	}
}

// A nil *bigtableMetrics must be a safe no-op so callers never need nil checks
// and metrics stay zero-overhead when no MeterProvider records them.
func TestBigtableMetricsNilSafe(t *testing.T) {
	var m *bigtableMetrics
	m.recordRead(context.Background(), "state", time.Millisecond, 2, 128)
	m.recordWrite(context.Background(), "state", time.Millisecond, 4, 256)
}

func TestBigtableMetricsRecordNoPanic(t *testing.T) {
	m := newBigtableMetrics()
	m.recordRead(context.Background(), "state", 5*time.Millisecond, 3, 512)
	m.recordWrite(context.Background(), "state", 7*time.Millisecond, 10, 4096)
	// Zero rows/bytes must still record latency without panicking.
	m.recordRead(context.Background(), "state", time.Millisecond, 0, 0)
}

func TestFallbackMetricsNilSafe(t *testing.T) {
	var m *fallbackMetrics
	m.recordRead("get", fallbackOutcomeCacheHit)
}

func TestFallbackMetricsRecordNoPanic(t *testing.T) {
	m := newFallbackMetrics()
	for _, outcome := range []string{
		fallbackOutcomeCacheHit,
		fallbackOutcomeBackendHit,
		fallbackOutcomeBackendMiss,
		fallbackOutcomeError,
	} {
		m.recordRead("get", outcome)
		m.recordRead("has", outcome)
	}
}
