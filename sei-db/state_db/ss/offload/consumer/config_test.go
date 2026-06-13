package consumer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigBackendName(t *testing.T) {
	require.Equal(t, backendScylla, (&Config{}).BackendName())
	require.Equal(t, backendBigtable, (&Config{Bigtable: BigtableConfig{ProjectID: "p"}}).BackendName())
	require.Equal(t, backendScylla, (&Config{Backend: "Scylla", Bigtable: BigtableConfig{ProjectID: "p"}}).BackendName())
}

func TestConfigValidateBigtable(t *testing.T) {
	cfg := Config{
		Backend: backendBigtable,
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
	require.ErrorContains(t, cfg.Validate(), backendBigtable)
}

func TestConfigApplyBackendDefaultsBigtable(t *testing.T) {
	cfg := Config{Backend: backendBigtable}
	cfg.applyBackendDefaults()
	require.Equal(t, defaultBigtableMaxBatchRecords, cfg.MaxBatchRecords)
	require.Equal(t, defaultBigtableBatchMaxWaitMS, cfg.BatchMaxWaitMS)

	cfg = Config{
		Backend:         backendBigtable,
		MaxBatchRecords: 32,
		BatchMaxWaitMS:  5,
	}
	cfg.applyBackendDefaults()
	require.Equal(t, 32, cfg.MaxBatchRecords)
	require.Equal(t, 5, cfg.BatchMaxWaitMS)
}

func TestConfigApplyBackendDefaultsScylla(t *testing.T) {
	cfg := Config{Backend: backendScylla}
	cfg.applyBackendDefaults()
	require.Zero(t, cfg.MaxBatchRecords)
	require.Zero(t, cfg.BatchMaxWaitMS)
}
