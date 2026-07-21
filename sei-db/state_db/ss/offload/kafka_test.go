package offload

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewKafkaStreamSupportsNilPublishAndClose(t *testing.T) {
	stream, err := NewKafkaStream(KafkaConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "historical-offload",
	})
	require.NoError(t, err)

	ack, err := stream.Publish(context.Background(), nil)
	require.NoError(t, err)
	require.True(t, ack.Accepted)

	closer, ok := stream.(interface{ Close() error })
	require.True(t, ok)
	require.NoError(t, closer.Close())
}
