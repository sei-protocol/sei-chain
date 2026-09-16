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
		metric.WithDescription("Receipts committed by the receipt store, by receipt status (success, reverted, failed)"),
		metric.WithUnit("{receipt}"),
	)),
}

const (
	receiptWriteStatusSuccess  = "success"
	receiptWriteStatusReverted = "reverted"
	receiptWriteStatusFailed   = "failed"
)

// receiptWriteStatuses is the closed label vocabulary of receipts_written_total.
// It matches the one txs_executed_total uses, so the two counters compare
// bucket for bucket.
var receiptWriteStatuses = []string{
	receiptWriteStatusSuccess,
	receiptWriteStatusReverted,
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

// receiptWriteStatus maps a receipt onto the bounded status label vocabulary used
// by receipts_written_total.
func receiptWriteStatus(receipt *types.Receipt) string {
	switch receipt.Status {
	case uint32(ethtypes.ReceiptStatusSuccessful):
		return receiptWriteStatusSuccess
	case uint32(ethtypes.ReceiptStatusFailed):
		return receiptWriteStatusReverted
	default:
		// The EVM writes only the two statuses above, so this bucket catches a
		// receipt no execution path is expected to produce.
		return receiptWriteStatusFailed
	}
}

// RecordReceiptsWritten reports a batch of written receipts by status, every
// status in the vocabulary including the ones the batch had none of. Callers
// record once the write path has accepted the batch, so a rejected write is not
// counted as a written one.
//
// A counter series exists only once something has recorded to it, and a global
// instrument drops measurements taken before the meter provider is installed, so
// a status is only reachable by a query if a batch reports it as zero.
func RecordReceiptsWritten(ctx context.Context, records []ReceiptRecord) {
	defer func() {
		if e := recover(); e != nil {
			fmt.Fprintf(os.Stderr, "telemetry panic: %v\n%s", e, debug.Stack())
		}
	}()
	counts := make(map[string]int64, len(receiptWriteStatuses))
	for _, record := range records {
		if record.Receipt == nil {
			continue
		}
		counts[receiptWriteStatus(record.Receipt)]++
	}
	for _, status := range receiptWriteStatuses {
		receiptMetrics.written.Add(ctx, counts[status], receiptWriteStatusAttr(status))
	}
}
