package consumer

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func TestKafkaReaderConfigApplyDefaults(t *testing.T) {
	cfg := KafkaReaderConfig{
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

func TestKafkaReaderConfigValidate(t *testing.T) {
	cfg := KafkaReaderConfig{}
	require.ErrorContains(t, cfg.Validate(), "brokers")
	cfg = KafkaReaderConfig{Brokers: []string{"x"}}
	require.ErrorContains(t, cfg.Validate(), "topic")
	cfg = KafkaReaderConfig{Brokers: []string{"x"}, Topic: "t"}
	require.ErrorContains(t, cfg.Validate(), "group id")
	cfg = KafkaReaderConfig{
		Brokers:     []string{"x"},
		Topic:       "t",
		GroupID:     "g",
		StartOffset: "middle",
	}
	require.ErrorContains(t, cfg.Validate(), "start offset")
}

func TestKafkaReaderConfigValidateSASL(t *testing.T) {
	base := KafkaReaderConfig{Brokers: []string{"x"}, Topic: "t", GroupID: "g"}

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

func TestDecodeEntry(t *testing.T) {
	entry := &proto.ChangelogEntry{Version: 7}
	payload, err := entry.Marshal()
	require.NoError(t, err)
	got, err := DecodeEntry(payload)
	require.NoError(t, err)
	require.Equal(t, int64(7), got.Version)
}
