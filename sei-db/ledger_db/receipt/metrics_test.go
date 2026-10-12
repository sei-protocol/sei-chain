package receipt

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

func bindTestReceiptMetrics(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("seidb_receipt")
	previous := receiptMetrics
	t.Cleanup(func() { receiptMetrics = previous })
	receiptMetrics.written = mustReceiptMetric(meter.Int64Counter("receipt_store_receipts_written_total"))
	return reader
}

func TestRecordReceiptsWrittenLabelsSuccessAndReverted(t *testing.T) {
	reader := bindTestReceiptMetrics(t)
	txHash := common.HexToHash("0x0100000000000000000000000000000000000000000000000000000000000001")
	RecordReceiptsWritten(t.Context(), []ReceiptRecord{
		{TxHash: txHash, Receipt: &evmtypes.Receipt{Status: uint32(ethtypes.ReceiptStatusSuccessful)}},
		{TxHash: txHash, Receipt: &evmtypes.Receipt{Status: uint32(ethtypes.ReceiptStatusFailed)}},
	})

	collected := collectReceiptMetrics(t, reader)
	require.Equal(t, int64(1), requireReceiptCounter(t, collected, "receipt_store_receipts_written_total",
		attribute.String("status", receiptWriteStatusSuccess)))
	require.Equal(t, int64(1), requireReceiptCounter(t, collected, "receipt_store_receipts_written_total",
		attribute.String("status", receiptWriteStatusReverted)))
}

// A run whose receipts all succeed still has to make the non-success buckets
// reachable, so a query can tell zero failures from an absent series.
func TestRecordReceiptsWrittenReportsEveryStatus(t *testing.T) {
	reader := bindTestReceiptMetrics(t)
	RecordReceiptsWritten(t.Context(), []ReceiptRecord{
		{Receipt: &evmtypes.Receipt{Status: uint32(ethtypes.ReceiptStatusSuccessful)}},
	})

	collected := collectReceiptMetrics(t, reader)
	for _, status := range receiptWriteStatuses {
		want := int64(0)
		if status == receiptWriteStatusSuccess {
			want = 1
		}
		require.Equal(t, want, requireReceiptCounter(t, collected, "receipt_store_receipts_written_total",
			attribute.String("status", status)), "status %s", status)
	}
}

func TestRecordReceiptsWrittenSkipsRecordsWithoutAReceipt(t *testing.T) {
	reader := bindTestReceiptMetrics(t)
	RecordReceiptsWritten(t.Context(), []ReceiptRecord{{TxHash: common.Hash{0x01}}})

	collected := collectReceiptMetrics(t, reader)
	for _, status := range receiptWriteStatuses {
		require.Equal(t, int64(0), requireReceiptCounter(t, collected, "receipt_store_receipts_written_total",
			attribute.String("status", status)), "status %s", status)
	}
}

// collectReceiptMetrics returns the collected metrics keyed by name.
func collectReceiptMetrics(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	collected := map[string]metricdata.Metrics{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			collected[m.Name] = m
		}
	}
	return collected
}

func requireReceiptCounter(t *testing.T, collected map[string]metricdata.Metrics, name string, attrs ...attribute.KeyValue) int64 {
	t.Helper()
	m, ok := collected[name]
	require.True(t, ok, "expected counter %s to be collected", name)
	sum, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok, "expected %s to be an int64 sum", name)
	var matched []metricdata.DataPoint[int64]
	for _, point := range sum.DataPoints {
		if hasAllReceiptAttributes(point.Attributes, attrs) {
			matched = append(matched, point)
		}
	}
	require.Len(t, matched, 1, "expected exactly one %s data point for %v", name, attrs)
	return matched[0].Value
}

func hasAllReceiptAttributes(set attribute.Set, attrs []attribute.KeyValue) bool {
	for _, want := range attrs {
		got, ok := set.Value(want.Key)
		if !ok || got != want.Value {
			return false
		}
	}
	return true
}
