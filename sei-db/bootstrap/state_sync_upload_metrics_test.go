package bootstrap

import (
	"context"
	"errors"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/sei-protocol/sei-chain/sei-db/statesync"
)

// meter replaces the metrics of u with metrics on a reader that the test collects.
func meter(t *testing.T, u *stateSyncUploader) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	u.metrics = newStateSyncUploadMetrics(provider.Meter("test"))
	return reader
}

// collect returns the data of each metric that reader holds, by name.
func collect(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Aggregation {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	data := map[string]metricdata.Aggregation{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			data[m.Name] = m.Data
		}
	}
	return data
}

// gauge returns the value of the int64 gauge name in data, and false when data has no such gauge.
func gauge(t *testing.T, data map[string]metricdata.Aggregation, name string) (int64, bool) {
	t.Helper()
	agg, ok := data[name]
	if !ok {
		return 0, false
	}
	g, ok := agg.(metricdata.Gauge[int64])
	require.True(t, ok, "%s is a %T", name, agg)
	require.Len(t, g.DataPoints, 1)
	return g.DataPoints[0].Value, true
}

// requireGauge asserts that the int64 gauge name in data is want.
func requireGauge(t *testing.T, data map[string]metricdata.Aggregation, name string, want int64) {
	t.Helper()
	got, ok := gauge(t, data, name)
	require.True(t, ok, "no %s", name)
	require.Equal(t, want, got, name)
}

// requireNoMetric asserts that data has no metric name.
func requireNoMetric(t *testing.T, data map[string]metricdata.Aggregation, name string) {
	t.Helper()
	_, ok := data[name]
	require.False(t, ok, "%s is recorded", name)
}

func TestStateSyncUploadMetricsRecordAnUpload(t *testing.T) {
	h := newUploadHome(t)
	now, _ := fixedClock(time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC))
	store := newFakeStore()
	store.now = now
	u := h.uploader(store)
	u.now = now
	reader := meter(t, u)
	height := h.commit(t)
	h.writeProof(t, height)

	require.NoError(t, u.sweep(t.Context()))

	data := collect(t, reader)
	requireGauge(t, data, "state_sync_upload_checkpoint_height", height)
	requireGauge(t, data, "state_sync_upload_latest_height", height)
	requireGauge(t, data, "state_sync_upload_latest_time", now().Unix())
	archive, ok := store.object(statesync.ArchiveKey(uploadChainID, height))
	require.True(t, ok)
	requireGauge(t, data, "state_sync_upload_archive_size", int64(len(archive)))
	duration, ok := data["state_sync_upload_duration"].(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, duration.DataPoints, 1)
	require.Equal(t, uint64(1), duration.DataPoints[0].Count)
	requireNoMetric(t, data, "state_sync_upload_failures")
}

// TestStateSyncUploadMetricsRecordTheArchiveOfAnotherNode runs a sweep on a node whose newest
// checkpoint is below the newest archive in the store. It records that archive and its time, and
// no upload.
func TestStateSyncUploadMetricsRecordTheArchiveOfAnotherNode(t *testing.T) {
	h := newUploadHome(t)
	now, advance := fixedClock(time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC))
	store := newFakeStore()
	store.now = now
	u := h.uploader(store)
	u.now = now
	reader := meter(t, u)
	height := h.commit(t)
	h.writeProof(t, height)
	store.set(statesync.LatestKey(uploadChainID), []byte(strconv.FormatInt(height+10, 10)))
	written := now()
	advance(time.Hour)

	require.NoError(t, u.sweep(t.Context()))

	data := collect(t, reader)
	requireGauge(t, data, "state_sync_upload_checkpoint_height", height)
	requireGauge(t, data, "state_sync_upload_latest_height", height+10)
	requireGauge(t, data, "state_sync_upload_latest_time", written.Unix())
	requireNoMetric(t, data, "state_sync_upload_archive_size")
	requireNoMetric(t, data, "state_sync_upload_duration")
}

// TestStateSyncUploadMetricsCountEachFailedSweep fails two sweeps with the same error. The node logs
// the error once and counts both sweeps.
func TestStateSyncUploadMetricsCountEachFailedSweep(t *testing.T) {
	h := newUploadHome(t)
	height := h.commit(t)
	h.writeProof(t, height)
	store := newFakeStore()
	store.put = func(_ context.Context, key string, _ io.Reader) error {
		if key == statesync.ArchiveKey(uploadChainID, height) {
			return errors.New("s3 is down")
		}
		return nil
	}
	u := h.uploader(store)
	reader := meter(t, u)

	lastErr := u.sweepLogged(t.Context(), "")
	require.NotEmpty(t, lastErr)
	require.Equal(t, lastErr, u.sweepLogged(t.Context(), lastErr))

	data := collect(t, reader)
	failures, ok := data["state_sync_upload_failures"].(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, failures.DataPoints, 1)
	require.Equal(t, int64(2), failures.DataPoints[0].Value)
	requireNoMetric(t, data, "state_sync_upload_latest_height")
	requireNoMetric(t, data, "state_sync_upload_archive_size")
}

func TestStateSyncUploadMetricsRecordTheStoreAtStart(t *testing.T) {
	h := newUploadHome(t)
	now, _ := fixedClock(time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC))
	store := newFakeStore()
	store.now = now
	store.set(statesync.LatestKey(uploadChainID), []byte("42\n"))
	u := h.uploader(store)
	reader := meter(t, u)

	u.logStoreAccess(t.Context())

	data := collect(t, reader)
	requireGauge(t, data, "state_sync_upload_latest_height", 42)
	requireGauge(t, data, "state_sync_upload_latest_time", now().Unix())
	requireNoMetric(t, data, "state_sync_upload_checkpoint_height")
}
