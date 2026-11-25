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

// uploadMetricsReader replaces the metrics of u with metrics on a reader that the test collects.
func uploadMetricsReader(t *testing.T, u *stateSyncUploader) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	u.metrics = newStateSyncUploadMetrics(provider.Meter("test"))
	return reader
}

// collectUploadMetrics returns the data of each metric that reader holds, by name.
func collectUploadMetrics(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Aggregation {
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

// requireUploadGauge asserts that the int64 gauge name in data is want.
func requireUploadGauge(t *testing.T, data map[string]metricdata.Aggregation, name string, want int64) {
	t.Helper()
	agg, ok := data[name]
	require.True(t, ok, "no %s", name)
	g, ok := agg.(metricdata.Gauge[int64])
	require.True(t, ok, "%s is a %T", name, agg)
	require.Len(t, g.DataPoints, 1)
	require.Equal(t, want, g.DataPoints[0].Value, name)
}

// requireNoUploadMetric asserts that data has no metric name.
func requireNoUploadMetric(t *testing.T, data map[string]metricdata.Aggregation, name string) {
	t.Helper()
	_, ok := data[name]
	require.False(t, ok, "%s is recorded", name)
}

// useSkewedClocks gives store and u fixed clocks, with the clock of u ten minutes ahead of the
// clock of store. It returns the clock of store, and a function that moves both clocks forward.
func useSkewedClocks(store *fakeStore, u *stateSyncUploader) (storeNow func() time.Time, advance func(time.Duration)) {
	start := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	storeNow, advanceStore := fixedClock(start)
	nodeNow, advanceNode := fixedClock(start.Add(10 * time.Minute))
	store.now = storeNow
	u.now = nodeNow
	return storeNow, func(d time.Duration) {
		advanceStore(d)
		advanceNode(d)
	}
}

// TestStateSyncUploadMetricsRecordAnUpload records the checkpoint, the archive and the time at which
// the store wrote latest.txt, not the time of the node.
func TestStateSyncUploadMetricsRecordAnUpload(t *testing.T) {
	h := newUploadHome(t)
	store := newFakeStore()
	u := h.uploader(store)
	storeNow, _ := useSkewedClocks(store, u)
	reader := uploadMetricsReader(t, u)
	height := h.commit(t)
	h.writeProof(t, height)

	require.NoError(t, u.sweep(t.Context()))

	data := collectUploadMetrics(t, reader)
	requireUploadGauge(t, data, "state_sync_upload_checkpoint_height", height)
	requireUploadGauge(t, data, "state_sync_upload_latest_height", height)
	requireUploadGauge(t, data, "state_sync_upload_latest_time", storeNow().Unix())
	archive, ok := store.object(statesync.ArchiveKey(uploadChainID, height))
	require.True(t, ok)
	requireUploadGauge(t, data, "state_sync_upload_archive_size", int64(len(archive)))
	duration, ok := data["state_sync_upload_duration"].(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, duration.DataPoints, 1)
	require.Equal(t, uint64(1), duration.DataPoints[0].Count)
	requireNoUploadMetric(t, data, "state_sync_upload_failures")
}

// TestStateSyncUploadMetricsRecordAHeightWrittenDuringTheUpload lets another node write a higher
// height to latest.txt while this node uploads. The node records that height and its time, also
// after later sweeps that do not read latest.txt.
func TestStateSyncUploadMetricsRecordAHeightWrittenDuringTheUpload(t *testing.T) {
	h := newUploadHome(t)
	height := h.commit(t)
	h.writeProof(t, height)
	store := newFakeStore()
	u := h.uploader(store)
	storeNow, advance := useSkewedClocks(store, u)
	reader := uploadMetricsReader(t, u)
	var written time.Time
	store.put = func(_ context.Context, key string, _ io.Reader) error {
		if key == statesync.ArchiveKey(uploadChainID, height) {
			store.set(statesync.LatestKey(uploadChainID), []byte(strconv.FormatInt(height+5, 10)))
			written = storeNow()
		}
		return nil
	}

	require.NoError(t, u.sweep(t.Context()))
	advance(time.Minute)
	require.NoError(t, u.sweep(t.Context()))

	data := collectUploadMetrics(t, reader)
	requireUploadGauge(t, data, "state_sync_upload_latest_height", height+5)
	requireUploadGauge(t, data, "state_sync_upload_latest_time", written.Unix())
}

// TestStateSyncUploadMetricsRecordTheArchiveOfAnotherNode runs a sweep on a node whose newest
// checkpoint is below the newest archive in the store. It records that archive and its time, and
// no upload.
func TestStateSyncUploadMetricsRecordTheArchiveOfAnotherNode(t *testing.T) {
	h := newUploadHome(t)
	store := newFakeStore()
	u := h.uploader(store)
	storeNow, advance := useSkewedClocks(store, u)
	reader := uploadMetricsReader(t, u)
	height := h.commit(t)
	h.writeProof(t, height)
	store.set(statesync.LatestKey(uploadChainID), []byte(strconv.FormatInt(height+10, 10)))
	written := storeNow()
	advance(time.Hour)

	require.NoError(t, u.sweep(t.Context()))

	data := collectUploadMetrics(t, reader)
	requireUploadGauge(t, data, "state_sync_upload_checkpoint_height", height)
	requireUploadGauge(t, data, "state_sync_upload_latest_height", height+10)
	requireUploadGauge(t, data, "state_sync_upload_latest_time", written.Unix())
	requireNoUploadMetric(t, data, "state_sync_upload_archive_size")
	requireNoUploadMetric(t, data, "state_sync_upload_duration")
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
	reader := uploadMetricsReader(t, u)

	lastErr := u.sweepLogged(t.Context(), "")
	require.NotEmpty(t, lastErr)
	require.Equal(t, lastErr, u.sweepLogged(t.Context(), lastErr))

	data := collectUploadMetrics(t, reader)
	failures, ok := data["state_sync_upload_failures"].(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, failures.DataPoints, 1)
	require.Equal(t, int64(2), failures.DataPoints[0].Value)
	requireNoUploadMetric(t, data, "state_sync_upload_latest_height")
	requireNoUploadMetric(t, data, "state_sync_upload_archive_size")
}

func TestStateSyncUploadMetricsRecordTheStoreAtStart(t *testing.T) {
	h := newUploadHome(t)
	store := newFakeStore()
	u := h.uploader(store)
	storeNow, _ := useSkewedClocks(store, u)
	store.set(statesync.LatestKey(uploadChainID), []byte("42\n"))
	reader := uploadMetricsReader(t, u)

	u.logStoreAccess(t.Context())

	data := collectUploadMetrics(t, reader)
	requireUploadGauge(t, data, "state_sync_upload_latest_height", 42)
	requireUploadGauge(t, data, "state_sync_upload_latest_time", storeNow().Unix())
	requireNoUploadMetric(t, data, "state_sync_upload_checkpoint_height")
}
