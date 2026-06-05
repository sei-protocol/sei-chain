package consumer

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	Kafka     KafkaReaderConfig
	Cockroach CockroachConfig
	Workers   int
	// ShardBufferSize is the backpressure point: when the sink stalls this
	// fills, the fetcher blocks, and Kafka stops being polled.
	ShardBufferSize int
	MaxBatchRecords int
	BatchMaxWaitMS  int
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
