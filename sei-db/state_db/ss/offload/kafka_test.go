package offload

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	dbproto "github.com/sei-protocol/sei-chain/sei-db/proto"
)

func TestKafkaConfigApplyDefaultsAndValidate(t *testing.T) {
	cfg := KafkaConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "historical-offload",
	}

	cfg.ApplyDefaults()
	require.Equal(t, "cryptosim-historical-offload", cfg.ClientID)
	require.Equal(t, "none", cfg.RequiredAcks)
	require.Equal(t, "snappy", cfg.Compression)
	require.Equal(t, 1000, cfg.BatchSize)
	require.Equal(t, 4<<20, cfg.BatchBytes)
	require.NoError(t, cfg.Validate())
}

func TestKafkaConfigValidateRequiresBrokerAndTopic(t *testing.T) {
	cfg := KafkaConfig{}
	require.Error(t, cfg.Validate())
}

func TestNewKafkaStreamSupportsNilPublishAndClose(t *testing.T) {
	stream, err := NewKafkaStream(KafkaConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "historical-offload",
	})
	require.NoError(t, err)

	ack, err := stream.Publish(context.Background(), nil)
	require.NoError(t, err)
	require.True(t, ack.Accepted)

	require.Error(t, stream.Replay(context.Background(), ReplayRequest{}, func(*dbproto.ChangelogEntry) error {
		return nil
	}))

	closer, ok := stream.(interface{ Close() error })
	require.True(t, ok)
	require.NoError(t, closer.Close())
}
