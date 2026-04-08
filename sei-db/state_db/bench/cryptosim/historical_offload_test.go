package cryptosim

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

func TestValidateHistoricalOffloadRequiresConfigForHistoricalOffloadBackend(t *testing.T) {
	cfg := DefaultCryptoSimConfig()
	cfg.DataDir = t.TempDir()
	cfg.LogDir = t.TempDir()
	cfg.Backend = wrappers.SSHistoricalOffload

	err := cfg.Validate()
	require.ErrorContains(t, err, "historical offload config is required")
}

func TestValidateHistoricalOffloadRequiresKafkaConfig(t *testing.T) {
	cfg := DefaultCryptoSimConfig()
	cfg.DataDir = t.TempDir()
	cfg.LogDir = t.TempDir()
	cfg.Backend = wrappers.SSHistoricalOffload
	cfg.HistoricalOffload = &wrappers.HistoricalOffloadConfig{
		Provider: "kafka",
	}

	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "historical offload kafka config is required")
}

func TestValidateHistoricalOffloadKafkaAcceptsMinimalValidConfig(t *testing.T) {
	cfg := DefaultCryptoSimConfig()
	cfg.DataDir = t.TempDir()
	cfg.LogDir = t.TempDir()
	cfg.Backend = wrappers.SSHistoricalOffload
	cfg.HistoricalOffload = &wrappers.HistoricalOffloadConfig{
		Provider: "kafka",
		Kafka: &wrappers.KafkaHistoricalOffloadConfig{
			Brokers: []string{"localhost:9092"},
			Topic:   "historical-offload",
		},
	}

	require.NoError(t, cfg.Validate())
	require.Equal(t, "cryptosim-historical-offload", cfg.HistoricalOffload.Kafka.ClientID)
	require.Equal(t, "none", cfg.HistoricalOffload.Kafka.RequiredAcks)
	require.Nil(t, cfg.HistoricalOffload.Kafka.Async)
	require.Equal(t, 1000, cfg.HistoricalOffload.Kafka.BatchSize)
	require.Equal(t, 4<<20, cfg.HistoricalOffload.Kafka.BatchBytes)
}

func TestValidateHistoricalOffloadKafkaIAMRequiresRegion(t *testing.T) {
	cfg := DefaultCryptoSimConfig()
	cfg.DataDir = t.TempDir()
	cfg.LogDir = t.TempDir()
	cfg.Backend = wrappers.SSHistoricalOffload
	cfg.HistoricalOffload = &wrappers.HistoricalOffloadConfig{
		Provider: "kafka",
		Kafka: &wrappers.KafkaHistoricalOffloadConfig{
			Brokers:       []string{"localhost:9098"},
			Topic:         "historical-offload",
			TLSEnabled:    true,
			SASLMechanism: "aws-msk-iam",
		},
	}

	err := cfg.Validate()
	require.ErrorContains(t, err, "region is required")

	cfg.HistoricalOffload.Kafka.Region = "eu-central-1"
	require.NoError(t, cfg.Validate())
}
