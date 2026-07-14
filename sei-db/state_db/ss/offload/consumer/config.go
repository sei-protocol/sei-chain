package consumer

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	backendScylla   = "scylla"
	backendBigtable = "bigtable"

	defaultBigtableMaxBatchRecords = 128
	defaultBigtableBatchMaxWaitMS  = 25
)

type Config struct {
	Backend         string
	Kafka           KafkaReaderConfig
	Scylla          ScyllaConfig
	Bigtable        BigtableConfig
	Workers         int
	ShardBufferSize int
	MaxBatchRecords int
	BatchMaxWaitMS  int
	// MetricsAddr, when set (e.g. ":2112"), serves Prometheus metrics at
	// /metrics so the backend cost counters (bigtable_rows_mutated_total,
	// bigtable_bytes_written_total, bigtable_mutate_latency_seconds, ...) can be
	// scraped. Empty disables the endpoint.
	MetricsAddr string
}

func (c *Config) Validate() error {
	if err := c.Kafka.Validate(); err != nil {
		return fmt.Errorf("kafka: %w", err)
	}
	// Match the node-side reader, which refuses to guess between two configured
	// backends; a consumer silently defaulting to Scylla here could ingest into
	// a different store than the node reads from.
	if strings.TrimSpace(c.Backend) == "" && c.Scylla.Configured() && c.Bigtable.Configured() {
		return fmt.Errorf("both scylla and bigtable are configured; set Backend to pick one")
	}
	switch c.BackendName() {
	case backendScylla:
		if err := c.Scylla.Validate(); err != nil {
			return fmt.Errorf("scylla: %w", err)
		}
	case backendBigtable:
		bigtable := c.Bigtable
		bigtable.ApplyDefaults()
		if err := bigtable.Validate(); err != nil {
			return fmt.Errorf("bigtable: %w", err)
		}
	default:
		return fmt.Errorf("unsupported backend %q", c.Backend)
	}
	if c.Workers < 0 {
		return fmt.Errorf("workers must be non-negative")
	}
	if c.ShardBufferSize < 0 {
		return fmt.Errorf("shard buffer size must be non-negative")
	}
	if c.MaxBatchRecords < 0 {
		return fmt.Errorf("max batch records must be non-negative")
	}
	if c.BatchMaxWaitMS < 0 {
		return fmt.Errorf("batch max wait ms must be non-negative")
	}
	return nil
}

func (c *Config) BackendName() string {
	backend := strings.ToLower(strings.TrimSpace(c.Backend))
	if backend != "" {
		return backend
	}
	if c.Bigtable.Configured() && !c.Scylla.Configured() {
		return backendBigtable
	}
	return backendScylla
}

func (c *Config) applyBackendDefaults() {
	if c.BackendName() != backendBigtable {
		return
	}
	if c.MaxBatchRecords == 0 {
		c.MaxBatchRecords = defaultBigtableMaxBatchRecords
	}
	if c.BatchMaxWaitMS == 0 {
		c.BatchMaxWaitMS = defaultBigtableBatchMaxWaitMS
	}
}

func NewSinkFromConfig(cfg Config) (Sink, error) {
	switch cfg.BackendName() {
	case backendScylla:
		return NewScyllaSink(cfg.Scylla)
	case backendBigtable:
		return NewBigtableSink(cfg.Bigtable)
	default:
		return nil, fmt.Errorf("unsupported backend %q", cfg.Backend)
	}
}

func LoadConfig(path string) (*Config, error) {
	// #nosec G304 -- config path is supplied by the operator on the command line.
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := &Config{}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyBackendDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
