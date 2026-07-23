package kafka

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriterConfigApplyDefaultsAndValidate(t *testing.T) {
	cfg := WriterConfig{
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

func TestWriterConfigValidateRequiresBrokerAndTopic(t *testing.T) {
	cfg := WriterConfig{}
	require.Error(t, cfg.Validate())
}

func TestWriterConfigValidateRequiresRegionAndTLSForAWSMSKIAM(t *testing.T) {
	cfg := WriterConfig{
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

func TestAWSMSKIAMMechanismRequiresMetadata(t *testing.T) {
	mech := &awsMSKIAMMechanism{region: "eu-central-1"}
	_, _, err := mech.Start(context.Background())
	require.ErrorContains(t, err, "missing sasl metadata")
}
