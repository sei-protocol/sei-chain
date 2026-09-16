package receipt

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var receiptMetrics = struct {
	written metric.Int64Counter
}{
	written: mustReceiptMetric(otel.Meter("seidb_receipt").Int64Counter(
		"receipts_written_total",
		metric.WithDescription("Receipt store writes by receipt status (success, failed)"),
		metric.WithUnit("{receipt}"),
	)),
}

func init() {
	ctx := context.Background()
	for _, status := range receiptWriteStatuses {
		receiptMetrics.written.Add(ctx, 0, receiptWriteStatusAttr(status))
	}
}

const (
	receiptWriteStatusSuccess = "success"
	receiptWriteStatusFailed  = "failed"
)

var receiptWriteStatuses = []string{
	receiptWriteStatusSuccess,
	receiptWriteStatusFailed,
}

var receiptWriteStatusOptions = receiptWriteStatusOptionTable()

func mustReceiptMetric[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}

func receiptWriteStatusOptionTable() map[string]metric.MeasurementOption {
	table := make(map[string]metric.MeasurementOption, len(receiptWriteStatuses))
	for _, status := range receiptWriteStatuses {
		table[status] = metric.WithAttributes(attribute.String("status", status))
	}
	return table
}

func receiptWriteStatusAttr(status string) metric.MeasurementOption {
	if option, ok := receiptWriteStatusOptions[status]; ok {
		return option
	}
	return receiptWriteStatusOptions[receiptWriteStatusFailed]
}

func receiptWriteStatus(receipt *types.Receipt) string {
	if receipt == nil {
		return receiptWriteStatusFailed
	}
	if receipt.Status == uint32(ethtypes.ReceiptStatusFailed) {
		return receiptWriteStatusFailed
	}
	return receiptWriteStatusSuccess
}

// RecordReceiptsWritten emits receipt write counts for a block write batch.
func RecordReceiptsWritten(ctx context.Context, records []ReceiptRecord) {
	defer func() {
		if e := recover(); e != nil {
			fmt.Fprintf(os.Stderr, "telemetry panic: %v\n%s", e, debug.Stack())
		}
	}()
	if len(records) == 0 {
		return
	}
	counts := make(map[string]int64, len(receiptWriteStatuses))
	for _, record := range records {
		if record.Receipt == nil {
			continue
		}
		counts[receiptWriteStatus(record.Receipt)]++
	}
	for status, count := range counts {
		if count > 0 {
			receiptMetrics.written.Add(ctx, count, receiptWriteStatusAttr(status))
		}
	}
}
