package consumer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigBackendName(t *testing.T) {
	require.Equal(t, backendScylla, (&Config{}).BackendName())
	require.Equal(t, backendFoundationDB, (&Config{FoundationDB: FoundationDBConfig{Enabled: true}}).BackendName())
	require.Equal(t, backendScylla, (&Config{Backend: "Scylla", FoundationDB: FoundationDBConfig{Enabled: true}}).BackendName())
}

func TestConfigValidateFoundationDB(t *testing.T) {
	cfg := Config{
		Backend: backendFoundationDB,
		Kafka: KafkaReaderConfig{
			Brokers: []string{"localhost:9092"},
			Topic:   "historical-offload",
			GroupID: "historical-foundationdb",
		},
		FoundationDB: FoundationDBConfig{
			Enabled: true,
		},
	}
	require.NoError(t, cfg.Validate())

	cfg.FoundationDB.APIVersion = 1
	require.ErrorContains(t, cfg.Validate(), backendFoundationDB)
}
