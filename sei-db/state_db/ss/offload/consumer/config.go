package consumer

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	backendScylla       = "scylla"
	backendFoundationDB = "foundationdb"
)

type Config struct {
	Kafka           KafkaReaderConfig
	Backend         string
	Scylla          ScyllaConfig
	FoundationDB    FoundationDBConfig
	Workers         int
	ShardBufferSize int
	MaxBatchRecords int
	BatchMaxWaitMS  int
}

func (c *Config) Validate() error {
	if err := c.Kafka.Validate(); err != nil {
		return fmt.Errorf("kafka: %w", err)
	}
	switch c.BackendName() {
	case backendScylla:
		if err := c.Scylla.Validate(); err != nil {
			return fmt.Errorf("scylla: %w", err)
		}
	case backendFoundationDB:
		fdb := c.FoundationDB
		fdb.ApplyDefaults()
		if err := fdb.Validate(); err != nil {
			return fmt.Errorf("foundationdb: %w", err)
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
	if c.FoundationDB.Configured() && !c.Scylla.Configured() {
		return backendFoundationDB
	}
	return backendScylla
}

func NewSinkFromConfig(cfg Config) (Sink, error) {
	switch cfg.BackendName() {
	case backendScylla:
		return NewScyllaSink(cfg.Scylla)
	case backendFoundationDB:
		return NewFoundationDBSink(cfg.FoundationDB)
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
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
