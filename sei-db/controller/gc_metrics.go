package controller

import (
	"context"
	"math"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	commonmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

const gcMeterName = "seidb_storage_gc"

var (
	gcMeter = otel.Meter(gcMeterName)

	gcPruneCycles = must(gcMeter.Int64Counter(
		"storage_gc_prune_cycles",
		metric.WithDescription("Number of prune cycles the storage garbage collector has run"),
		metric.WithUnit("{count}"),
	))

	gcSnapshotCutLine = must(gcMeter.Int64Gauge(
		"storage_gc_snapshot_cut_line",
		metric.WithDescription("Height the last prune cycle pruned snapshots below"),
		metric.WithUnit("{height}"),
	))

	gcHistoryCutLine = must(gcMeter.Int64Gauge(
		"storage_gc_history_cut_line",
		metric.WithDescription("Height the last prune cycle pruned history below; 0 retains all history"),
		metric.WithUnit("{height}"),
	))

	gcRollbackFloor = must(gcMeter.Int64Gauge(
		"storage_gc_rollback_floor",
		metric.WithDescription("Earliest height each store reported a rollback may target, by store"),
		metric.WithUnit("{height}"),
	))

	gcStorePruneDuration = must(gcMeter.Float64Histogram(
		"storage_gc_store_prune_duration",
		metric.WithDescription("Time one store took to prune its snapshots and history in a cycle, by store"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LongLatencyBuckets...),
	))

	gcStorePruneErrors = must(gcMeter.Int64Counter(
		"storage_gc_store_prune_errors",
		metric.WithDescription("Number of prune cycles in which a store failed to prune, by store"),
		metric.WithUnit("{count}"),
	))
)

// recordCycle records one cycle's decisions: each store's rollback floor and the cut lines taken from
// them.
func recordCycle(stores []PrunableStore, decisions []storeDecision, snapshotCutLine uint64, historyCutLine uint64) {
	ctx := context.Background()
	gcPruneCycles.Add(ctx, 1)
	gcSnapshotCutLine.Record(ctx, clampToInt64(snapshotCutLine))
	gcHistoryCutLine.Record(ctx, clampToInt64(historyCutLine))
	for i, store := range stores {
		gcRollbackFloor.Record(ctx, clampToInt64(decisions[i].floor), storeAttribute(store))
	}
}

// recordStorePrune records how long one store took to prune in a cycle, and whether it failed.
func recordStorePrune(store PrunableStore, elapsed time.Duration, err error) {
	ctx := context.Background()
	gcStorePruneDuration.Record(ctx, elapsed.Seconds(), storeAttribute(store))
	if err != nil {
		gcStorePruneErrors.Add(ctx, 1, storeAttribute(store))
	}
}

func storeAttribute(store PrunableStore) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("store", store.Name()))
}

func clampToInt64(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
