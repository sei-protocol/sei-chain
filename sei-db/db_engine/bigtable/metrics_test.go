package bigtable

import (
	"context"
	"testing"
	"time"
)

func TestRowSize(t *testing.T) {
	row := Row{
		Key: "abc", // 3
		Cells: []Cell{
			{Qualifier: "value", Value: []byte("hello")}, // 5 + 5
			{Qualifier: "deleted", Value: []byte{0x1}},   // 7 + 1
		},
	}
	if got, want := rowSize(row), int64(3+5+5+7+1); got != want {
		t.Fatalf("rowSize() = %d, want %d", got, want)
	}
	if got := rowSize(Row{}); got != 0 {
		t.Fatalf("rowSize(empty) = %d, want 0", got)
	}
}

func TestMutationSize(t *testing.T) {
	row := RowMutation{
		RowKey: "row1", // 4
		SetCells: []SetCell{
			{Qualifier: "value", Value: []byte("data")}, // 5 + 4
		},
	}
	if got, want := mutationSize(row), int64(4+5+4); got != want {
		t.Fatalf("mutationSize() = %d, want %d", got, want)
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
