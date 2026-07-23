// Package kafka bundles the Kafka transport used by the historical state
// offload: producer (writer) construction, consumer (reader) construction,
// and the generic batching consume loop that drains a topic into a Sink.
package kafka

import (
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/compress"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
)

const kafkaOptionNone = "none"

type WriterConfig struct {
	Brokers      []string
	Topic        string
	ClientID     string
	Region       string
	Async        bool
	RequiredAcks string
	Compression  string
	BatchSize    int
	BatchTimeout time.Duration
	BatchBytes   int
	TLSEnabled   bool
	// SASLMechanism selects broker auth: "none", "plain" (username/password,
	// e.g. Google Cloud Managed Kafka service-account credentials), or
	// "aws-msk-iam".
	SASLMechanism string
	Username      string
	Password      string
}

func (c *WriterConfig) ApplyDefaults() {
	if c.ClientID == "" {
		c.ClientID = "cryptosim-historical-offload"
	}
	if c.RequiredAcks == "" {
		c.RequiredAcks = kafkaOptionNone
	}
	if c.Compression == "" {
		c.Compression = "snappy"
	}
	if c.BatchSize == 0 {
		c.BatchSize = 1000
	}
	if c.BatchTimeout == 0 {
		c.BatchTimeout = 50 * time.Millisecond
	}
	if c.BatchBytes == 0 {
		c.BatchBytes = 4 << 20
	}
}

func (c *WriterConfig) Validate() error {
	if len(c.Brokers) == 0 {
		return fmt.Errorf("kafka brokers are required")
	}
	if c.Topic == "" {
		return fmt.Errorf("kafka topic is required")
	}
	if c.BatchTimeout < 0 {
		return fmt.Errorf("kafka batch timeout must be non-negative")
	}
	if c.BatchSize < 0 {
		return fmt.Errorf("kafka batch size must be non-negative")
	}
	if c.BatchBytes < 0 {
		return fmt.Errorf("kafka batch bytes must be non-negative")
	}

	switch strings.ToLower(c.RequiredAcks) {
	case kafkaOptionNone, "leader", "all":
	default:
		return fmt.Errorf("unsupported kafka required acks %q", c.RequiredAcks)
	}

	switch strings.ToLower(c.Compression) {
	case kafkaOptionNone, "gzip", "snappy", "lz4", "zstd":
	default:
		return fmt.Errorf("unsupported kafka compression %q", c.Compression)
	}

	return ValidateSASL(*c)
}

// ValidateSASL checks the SASL mechanism and the credentials it requires.
// Shared by the producer and the offload consumer configs.
func ValidateSASL(cfg WriterConfig) error {
	switch strings.ToLower(cfg.SASLMechanism) {
	case "", kafkaOptionNone:
	case "plain":
		if cfg.Username == "" || cfg.Password == "" {
			return fmt.Errorf("kafka username and password are required for sasl plain")
		}
	case "aws-msk-iam":
		if !cfg.TLSEnabled {
			return fmt.Errorf("kafka tls must be enabled for aws-msk-iam")
		}
		if cfg.Region == "" {
			return fmt.Errorf("kafka region is required for aws-msk-iam")
		}
	default:
		return fmt.Errorf("unsupported kafka sasl mechanism %q", cfg.SASLMechanism)
	}
	return nil
}

// NewWriter builds a kafka-go Writer from cfg after applying defaults and
// validating it.
func NewWriter(cfg WriterConfig) (*kafkago.Writer, error) {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	dialer := &kafkago.Dialer{
		ClientID: cfg.ClientID,
		Timeout:  10 * time.Second,
	}
	if cfg.TLSEnabled {
		dialer.TLS = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	mechanism, err := NewSASLMechanism(cfg)
	if err != nil {
		return nil, err
	}
	dialer.SASLMechanism = mechanism

	return &kafkago.Writer{
		Addr:         kafkago.TCP(cfg.Brokers...),
		Topic:        cfg.Topic,
		Balancer:     &kafkago.Hash{},
		RequiredAcks: kafkaRequiredAcks(cfg.RequiredAcks),
		BatchSize:    cfg.BatchSize,
		BatchTimeout: cfg.BatchTimeout,
		BatchBytes:   int64(cfg.BatchBytes),
		Async:        cfg.Async,
		Compression:  kafkaCompression(cfg.Compression),
		Transport: &kafkago.Transport{
			ClientID:    cfg.ClientID,
			TLS:         dialer.TLS,
			SASL:        mechanism,
			IdleTimeout: 30 * time.Second,
			MetadataTTL: 30 * time.Second,
			DialTimeout: dialer.Timeout,
		},
	}, nil
}

func kafkaRequiredAcks(requiredAcks string) kafkago.RequiredAcks {
	switch strings.ToLower(requiredAcks) {
	case kafkaOptionNone:
		return kafkago.RequireNone
	case "leader":
		return kafkago.RequireOne
	default:
		return kafkago.RequireAll
	}
}

func kafkaCompression(name string) compress.Compression {
	switch strings.ToLower(name) {
	case "gzip":
		return compress.Gzip
	case "lz4":
		return compress.Lz4
	case "zstd":
		return compress.Zstd
	case kafkaOptionNone:
		return compress.None
	default:
		return compress.Snappy
	}
}

// NewSASLMechanism returns the SASL mechanism for cfg, which must already
// have passed ValidateSASL.
func NewSASLMechanism(cfg WriterConfig) (sasl.Mechanism, error) {
	switch strings.ToLower(cfg.SASLMechanism) {
	case "", kafkaOptionNone:
		return nil, nil
	case "plain":
		return plain.Mechanism{Username: cfg.Username, Password: cfg.Password}, nil
	case "aws-msk-iam":
		return newAWSMSKIAMMechanism(cfg)
	default:
		return nil, fmt.Errorf("unsupported kafka sasl mechanism %q", cfg.SASLMechanism)
	}
}
