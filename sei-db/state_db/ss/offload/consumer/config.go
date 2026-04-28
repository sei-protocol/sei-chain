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
	// Workers sets per-partition write parallelism. 0 or 1 means serial.
	Workers int
}

func (c *Config) Validate() error {
	if err := c.Kafka.Validate(); err != nil {
		return fmt.Errorf("kafka: %w", err)
	}
	if err := c.Cockroach.Validate(); err != nil {
		return fmt.Errorf("cockroach: %w", err)
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
