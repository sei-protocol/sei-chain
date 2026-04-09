package offload

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
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

func TestKafkaConfigValidateRequiresRegionAndTLSForAWSMSKIAM(t *testing.T) {
	cfg := KafkaConfig{
		Brokers:       []string{"localhost:9098"},
		Topic:         "historical-offload",
		SASLMechanism: "aws-msk-iam",
	}
	cfg.ApplyDefaults()

	require.ErrorContains(t, cfg.Validate(), "tls must be enabled")

	cfg.TLSEnabled = true
	require.ErrorContains(t, cfg.Validate(), "region is required")

	cfg.Region = "eu-central-1"
	require.NoError(t, cfg.Validate())
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

	closer, ok := stream.(interface{ Close() error })
	require.True(t, ok)
	require.NoError(t, closer.Close())
}

func TestAWSMSKIAMMechanismRequiresMetadata(t *testing.T) {
	mech := &awsMSKIAMMechanism{region: "eu-central-1"}
	_, _, err := mech.Start(context.Background())
	require.ErrorContains(t, err, "missing sasl metadata")
}
