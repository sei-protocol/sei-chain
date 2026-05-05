package consumer

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config is the top-level JSON config for the consumer binary.
type Config struct {
	Kafka     KafkaReaderConfig
	Cockroach CockroachConfig
	// Workers sets per-partition write parallelism. 0 picks the default.
	Workers int
	// ShardBufferSize bounds the per-worker in-flight queue. Operates as
	// the backpressure point: when the sink stalls, this fills, the
	// fetcher blocks, and Kafka stops being polled. 0 picks the default.
	ShardBufferSize int
	// MaxBatchRecords caps records per sink write. 0 picks the default.
	MaxBatchRecords int
	// BatchMaxWaitMS caps how long a worker waits to fill a partial batch.
	// 0 picks the default.
	BatchMaxWaitMS int
}

func (c *Config) Validate() error {
	if err := c.Kafka.Validate(); err != nil {
		return fmt.Errorf("kafka: %w", err)
	}
	if err := c.Cockroach.Validate(); err != nil {
		return fmt.Errorf("cockroach: %w", err)
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

// LoadConfig reads a JSON config file from path and validates it.
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
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
