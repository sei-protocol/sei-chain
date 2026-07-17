package consumer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigValidate(t *testing.T) {
	cfg := Config{
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

func TestConfigApplyDefaults(t *testing.T) {
	cfg := Config{}
	cfg.applyDefaults()
	require.Equal(t, defaultMaxBatchRecords, cfg.MaxBatchRecords)
	require.Equal(t, defaultBatchMaxWaitMS, cfg.BatchMaxWaitMS)

	cfg = Config{MaxBatchRecords: 32, BatchMaxWaitMS: 5}
	cfg.applyDefaults()
	require.Equal(t, 32, cfg.MaxBatchRecords)
	require.Equal(t, 5, cfg.BatchMaxWaitMS)
}

func TestLoadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"Kafka": {"Brokers": ["localhost:9092"], "Topic": "historical-offload", "GroupID": "g"},
		"Bigtable": {"ProjectID": "p", "InstanceID": "i", "Table": "t"},
		"MetricsAddr": ":2112"
	}`), 0o600))

	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	require.Equal(t, ":2112", cfg.MetricsAddr)
	require.Equal(t, defaultMaxBatchRecords, cfg.MaxBatchRecords)
}
