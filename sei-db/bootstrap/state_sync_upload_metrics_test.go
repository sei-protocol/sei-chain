package bootstrap

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
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

// requireUploadFailures asserts that the failure count in data is want, by stage.
func requireUploadFailures(t *testing.T, data map[string]metricdata.Aggregation, want map[string]int64) {
	t.Helper()
	failures, ok := data["state_sync_upload_failures"].(metricdata.Sum[int64])
	require.True(t, ok, "no state_sync_upload_failures")
	got := map[string]int64{}
	for _, p := range failures.DataPoints {
		stage, ok := p.Attributes.Value("stage")
		require.True(t, ok, "a failure has no stage")
		got[stage.AsString()] = p.Value
	}
	require.Equal(t, want, got)
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
	requireUploadGauge(t, data, "state_sync_upload_local_checkpoint_height", height)
	requireUploadGauge(t, data, "state_sync_upload_bucket_height", height)
	requireUploadGauge(t, data, "state_sync_upload_bucket_time", storeNow().Unix())
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
	requireUploadGauge(t, data, "state_sync_upload_bucket_height", height+5)
	requireUploadGauge(t, data, "state_sync_upload_bucket_time", written.Unix())
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
	requireUploadGauge(t, data, "state_sync_upload_local_checkpoint_height", height)
	requireUploadGauge(t, data, "state_sync_upload_bucket_height", height+10)
	requireUploadGauge(t, data, "state_sync_upload_bucket_time", written.Unix())
	requireNoUploadMetric(t, data, "state_sync_upload_archive_size")
	requireNoUploadMetric(t, data, "state_sync_upload_duration")
}

// TestStateSyncUploadMetricsCountEachFailedSweep fails two sweeps with the same error. The node logs
// the error once and counts both sweeps at the put stage.
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
	requireUploadFailures(t, data, map[string]int64{"put": 2})
	requireNoUploadMetric(t, data, "state_sync_upload_bucket_height")
	requireNoUploadMetric(t, data, "state_sync_upload_archive_size")
}

// TestStateSyncUploadMetricsCountAFailureAtItsStage fails one step of a sweep in each case, and
// counts the failure at the stage of that step.
func TestStateSyncUploadMetricsCountAFailureAtItsStage(t *testing.T) {
	failPut := func(failKey func(height int64) string) func(*testing.T, *uploadHome, *fakeStore, int64) {
		return func(_ *testing.T, _ *uploadHome, store *fakeStore, height int64) {
			store.put = func(_ context.Context, key string, _ io.Reader) error {
				if key == failKey(height) {
					return errors.New("s3 is down")
				}
				return nil
			}
		}
	}
	for _, tc := range []struct {
		stage string
		fail  func(t *testing.T, h *uploadHome, store *fakeStore, height int64)
	}{
		{"checkpoint", func(t *testing.T, h *uploadHome, _ *fakeStore, _ int64) {
			require.NoError(t, os.RemoveAll(h.proofDir))
			require.NoError(t, os.WriteFile(h.proofDir, nil, 0o600))
		}},
		{"read", func(_ *testing.T, _ *uploadHome, store *fakeStore, _ int64) {
			store.set(statesync.LatestKey(uploadChainID), []byte("not a height"))
		}},
		{"claim", failPut(func(int64) string { return statesync.ClaimKey(uploadChainID) })},
		{"stage", func(t *testing.T, h *uploadHome, _ *fakeStore, height int64) {
			manifests, err := filepath.Glob(filepath.Join(h.flatKVDir, flatkv.SnapshotDirName(height), "account", "MANIFEST-*"))
			require.NoError(t, err)
			require.NotEmpty(t, manifests)
			for _, m := range manifests {
				require.NoError(t, os.Remove(m))
			}
		}},
		{"put", failPut(func(height int64) string { return statesync.ArchiveKey(uploadChainID, height) })},
		{"advance", failPut(func(int64) string { return statesync.LatestKey(uploadChainID) })},
	} {
		t.Run(tc.stage, func(t *testing.T) {
			h := newUploadHome(t)
			height := h.commit(t)
			h.writeProof(t, height)
			store := newFakeStore()
			tc.fail(t, h, store, height)
			u := h.uploader(store)
			reader := uploadMetricsReader(t, u)

			require.NotEmpty(t, u.sweepLogged(t.Context(), ""))

			requireUploadFailures(t, collectUploadMetrics(t, reader), map[string]int64{tc.stage: 1})
		})
	}
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
	requireUploadGauge(t, data, "state_sync_upload_bucket_height", 42)
	requireUploadGauge(t, data, "state_sync_upload_bucket_time", storeNow().Unix())
	requireNoUploadMetric(t, data, "state_sync_upload_local_checkpoint_height")
}
