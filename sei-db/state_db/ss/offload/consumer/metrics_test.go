package consumer

import (
	"context"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"
)

func TestBatchLag(t *testing.T) {
	// Max over the batch of (HighWaterMark - Offset - 1); the head message
	// (offset == HWM-1) has lag 0.
	msgs := []kafka.Message{
		{Offset: 10, HighWaterMark: 100}, // 89
		{Offset: 98, HighWaterMark: 100}, // 1
		{Offset: 99, HighWaterMark: 100}, // 0 (head)
	}
	require.Equal(t, int64(89), batchLag(msgs))

	// No watermark information -> sentinel -1 so the gauge is left untouched.
	require.Equal(t, int64(-1), batchLag([]kafka.Message{{Offset: 5}}))
	require.Equal(t, int64(-1), batchLag(nil))
}

// recordBatch must be a safe no-op on a nil receiver and must not panic when
// the global MeterProvider is the default no-op provider.
func TestConsumerMetricsRecordNoPanic(t *testing.T) {
	var nilM *consumerMetrics
	nilM.recordBatch(context.Background(), 5, 3, time.Millisecond)

	m := newConsumerMetrics()
	m.recordBatch(context.Background(), 128, 0, 12*time.Millisecond)
	m.recordBatch(context.Background(), 0, -1, time.Millisecond) // zero records, unknown lag
}
