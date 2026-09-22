package pebbledb

import (
	"context"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// CommitMetrics reports where the time inside a pebble batch commit went, split into the phases pebble
// attributes in BatchCommitStats. All methods are nil-safe, so callers report unconditionally whether or
// not metrics are enabled.
//
// Pebble's Metrics struct carries no stall information at all, and write stalls are otherwise observable
// only through an EventListener. Without this, a commit that was slow doing work cannot be told apart
// from one that sat waiting for a memtable flush or for L0 compaction to catch up — which are different
// problems with different fixes.
type CommitMetrics struct {
	// Seconds charged to each phase of a commit, carrying a "phase" attribute.
	phaseDuration metric.Float64Counter

	// Commits observed, so the phase totals can be read as a per-commit average.
	commits metric.Int64Counter

	// Identifies the database these measurements belong to.
	dbAttr attribute.KeyValue
}

// NewCommitMetrics creates a CommitMetrics for the named database, or nil when disabled.
func NewCommitMetrics(enabled bool, databaseName string) *CommitMetrics {
	if !enabled {
		return nil
	}

	meter := otel.Meter(pebbleMeterName)
	phaseDuration, _ := meter.Float64Counter(
		"pebble_commit_phase_duration",
		metric.WithDescription("Time spent in each phase of a pebble batch commit, as attributed by pebble"),
		metric.WithUnit("s"),
	)
	commits, _ := meter.Int64Counter(
		"pebble_commit_count",
		metric.WithDescription("Pebble batch commits observed"),
		metric.WithUnit("{count}"),
	)

	return &CommitMetrics{
		phaseDuration: phaseDuration,
		commits:       commits,
		dbAttr:        attribute.String("db", databaseName),
	}
}

// Record reports one commit's phase breakdown.
func (m *CommitMetrics) Record(stats pebble.BatchCommitStats) {
	if m == nil {
		return
	}

	ctx := context.Background()
	m.commits.Add(ctx, 1, metric.WithAttributes(m.dbAttr))

	m.addPhase(ctx, "semaphore_wait", stats.SemaphoreWaitDuration)
	m.addPhase(ctx, "wal_queue_wait", stats.WALQueueWaitDuration)
	m.addPhase(ctx, "memtable_write_stall", stats.MemTableWriteStallDuration)
	m.addPhase(ctx, "l0_read_amp_write_stall", stats.L0ReadAmpWriteStallDuration)
	m.addPhase(ctx, "wal_rotation", stats.WALRotationDuration)
	m.addPhase(ctx, "commit_wait", stats.CommitWaitDuration)

	// Pebble does not break out every queue a commit waits in, so the remainder covers the commit's
	// real work plus whatever it leaves unattributed. Clamped because the total is measured separately
	// from the parts and can arrive slightly below their sum.
	attributed := stats.SemaphoreWaitDuration + stats.WALQueueWaitDuration +
		stats.MemTableWriteStallDuration + stats.L0ReadAmpWriteStallDuration +
		stats.WALRotationDuration + stats.CommitWaitDuration
	m.addPhase(ctx, "unattributed", stats.TotalDuration-attributed)
}

// addPhase charges a duration to one phase, skipping the zero case that most phases report most of the
// time.
func (m *CommitMetrics) addPhase(ctx context.Context, phase string, duration time.Duration) {
	if duration <= 0 {
		return
	}
	m.phaseDuration.Add(ctx, duration.Seconds(),
		metric.WithAttributes(m.dbAttr, attribute.String("phase", phase)))
}
