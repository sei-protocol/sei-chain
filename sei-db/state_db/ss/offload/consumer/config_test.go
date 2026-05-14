package consumer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigBackendName(t *testing.T) {
	require.Equal(t, "scylla", (&Config{}).BackendName())
	require.Equal(t, "bigtable", (&Config{Bigtable: BigtableConfig{ProjectID: "p"}}).BackendName())
	require.Equal(t, "scylla", (&Config{Backend: "Scylla", Bigtable: BigtableConfig{ProjectID: "p"}}).BackendName())
}

func TestConfigValidateBigtable(t *testing.T) {
	cfg := Config{
		Backend: "bigtable",
		Kafka: KafkaReaderConfig{
			Brokers: []string{"localhost:9092"},
			Topic:   "historical-offload",
			GroupID: "historical-bigtable",
		},
		Bigtable: BigtableConfig{
			ProjectID:  "project",
			InstanceID: "instance",
			Table:      "state",
		},
	}
	require.NoError(t, cfg.Validate())

	cfg.Bigtable.Table = ""
	require.ErrorContains(t, cfg.Validate(), "bigtable")
}
