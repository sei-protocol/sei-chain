package consumer

import (
	"os"
	"path/filepath"
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

func TestConfigValidateRejectsAmbiguousDualBackends(t *testing.T) {
	cfg := Config{
		Kafka: KafkaReaderConfig{
			Brokers: []string{"localhost:9092"},
			Topic:   "historical-offload",
			GroupID: "g",
		},
		Scylla:   ScyllaConfig{Hosts: []string{"127.0.0.1"}, Keyspace: "ks"},
		Bigtable: BigtableConfig{ProjectID: "p", InstanceID: "i", Table: "t"},
	}
	require.ErrorContains(t, cfg.Validate(), "both scylla and bigtable")

	cfg.Backend = backendBigtable
	require.NoError(t, cfg.Validate())
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

func TestLoadConfigMetricsAddr(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"Backend": "bigtable",
		"Kafka": {"Brokers": ["localhost:9092"], "Topic": "historical-offload", "GroupID": "g"},
		"Bigtable": {"ProjectID": "p", "InstanceID": "i", "Table": "t"},
		"MetricsAddr": ":9092"
	}`), 0o600))

	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	require.Equal(t, ":9092", cfg.MetricsAddr)
}
