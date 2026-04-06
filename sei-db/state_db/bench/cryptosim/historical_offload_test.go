package cryptosim

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

func TestValidateHistoricalOffloadAllowsLocalDefault(t *testing.T) {
	cfg := DefaultCryptoSimConfig()
	cfg.DataDir = t.TempDir()
	cfg.LogDir = t.TempDir()
	cfg.Backend = wrappers.SSHistoricalOffload

	require.NoError(t, cfg.Validate())
}

func TestValidateHistoricalOffloadRequiresKafkaConfig(t *testing.T) {
	cfg := DefaultCryptoSimConfig()
	cfg.DataDir = t.TempDir()
	cfg.LogDir = t.TempDir()
	cfg.Backend = wrappers.SSHistoricalOffload
	cfg.HistoricalOffload = &HistoricalOffloadConfig{
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
	cfg.HistoricalOffload = &HistoricalOffloadConfig{
		Provider: "kafka",
		Kafka: &KafkaHistoricalOffloadConfig{
			Brokers: []string{"localhost:9092"},
			Topic:   "historical-offload",
		},
	}

	require.NoError(t, cfg.Validate())
	require.Equal(t, "cryptosim-historical-offload", cfg.HistoricalOffload.Kafka.ClientID)
	require.Equal(t, "all", cfg.HistoricalOffload.Kafka.RequiredAcks)
}
