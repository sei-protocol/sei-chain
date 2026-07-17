package consumer

import (
	"encoding/json"
	"fmt"
	"os"
)

const (
	defaultMaxBatchRecords = 128
	defaultBatchMaxWaitMS  = 25
)

type Config struct {
	Kafka           KafkaReaderConfig
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
	bigtable := c.Bigtable
	bigtable.ApplyDefaults()
	if err := bigtable.Validate(); err != nil {
		return fmt.Errorf("bigtable: %w", err)
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

func (c *Config) applyDefaults() {
	if c.MaxBatchRecords == 0 {
		c.MaxBatchRecords = defaultMaxBatchRecords
	}
	if c.BatchMaxWaitMS == 0 {
		c.BatchMaxWaitMS = defaultBatchMaxWaitMS
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
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
