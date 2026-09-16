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
	receiptMetrics.written = mustReceiptMetric(meter.Int64Counter("receipts_written_total"))
	return reader
}

func TestRecordReceiptsWrittenLabelsSuccessAndFailed(t *testing.T) {
	reader := bindTestReceiptMetrics(t)
	txHash := common.HexToHash("0x0100000000000000000000000000000000000000000000000000000000000001")
	RecordReceiptsWritten(t.Context(), []ReceiptRecord{
		{TxHash: txHash, Receipt: &evmtypes.Receipt{Status: uint32(ethtypes.ReceiptStatusSuccessful)}},
		{TxHash: txHash, Receipt: &evmtypes.Receipt{Status: uint32(ethtypes.ReceiptStatusFailed)}},
	})

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	collected := map[string]metricdata.Metrics{}
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			collected[metric.Name] = metric
		}
	}
	require.Equal(t, int64(1), requireReceiptCounter(t, collected, "receipts_written_total",
		attribute.String("status", receiptWriteStatusSuccess)))
	require.Equal(t, int64(1), requireReceiptCounter(t, collected, "receipts_written_total",
		attribute.String("status", receiptWriteStatusFailed)))
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
