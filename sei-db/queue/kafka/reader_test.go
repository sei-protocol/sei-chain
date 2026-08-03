package kafka

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReaderConfigApplyDefaults(t *testing.T) {
	cfg := ReaderConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "historical-offload",
		GroupID: "bigtable",
	}
	cfg.ApplyDefaults()
	require.Equal(t, "sei-historical-offload-consumer", cfg.ClientID)
	require.Equal(t, "first", cfg.StartOffset)
	require.Equal(t, 1, cfg.MinBytes)
	require.Equal(t, 10<<20, cfg.MaxBytes)
}

func TestReaderConfigValidate(t *testing.T) {
	cfg := ReaderConfig{}
	require.ErrorContains(t, cfg.Validate(), "brokers")
	cfg = ReaderConfig{Brokers: []string{"x"}}
	require.ErrorContains(t, cfg.Validate(), "topic")
	cfg = ReaderConfig{Brokers: []string{"x"}, Topic: "t"}
	require.ErrorContains(t, cfg.Validate(), "group id")
	cfg = ReaderConfig{
		Brokers:     []string{"x"},
		Topic:       "t",
		GroupID:     "g",
		StartOffset: "middle",
	}
	require.ErrorContains(t, cfg.Validate(), "start offset")
}

func TestReaderConfigValidateSASL(t *testing.T) {
	base := ReaderConfig{Brokers: []string{"x"}, Topic: "t", GroupID: "g"}

	cfg := base
	cfg.SASLMechanism = "aws-msk-iam"
	require.ErrorContains(t, cfg.Validate(), "tls")

	cfg.TLSEnabled = true
	require.ErrorContains(t, cfg.Validate(), "region")

	cfg.Region = "us-east-1"
	require.NoError(t, cfg.Validate())

	// SASL/PLAIN (e.g. Google Cloud Managed Kafka) needs credentials.
	cfg = base
	cfg.SASLMechanism = "plain"
	require.ErrorContains(t, cfg.Validate(), "username and password")

	cfg.Username = "svc@project.iam.gserviceaccount.com"
	cfg.Password = "base64-encoded-key"
	require.NoError(t, cfg.Validate())

	cfg = base
	cfg.SASLMechanism = "scram"
	require.ErrorContains(t, cfg.Validate(), "sasl mechanism")

	cfg = base
	cfg.SASLMechanism = "none"
	require.NoError(t, cfg.Validate())
}
