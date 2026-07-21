package kafka

import (
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

// TLS/SASL must match the producer cluster. Commits are synchronous
// (kafka-go's zero CommitInterval) so offsets only advance after the sink
// persists each entry.
type ReaderConfig struct {
	Brokers     []string
	Topic       string
	GroupID     string
	ClientID    string
	Region      string
	StartOffset string // "first" or "last"; defaults to "first"
	MinBytes    int
	MaxBytes    int
	MaxWait     time.Duration
	TLSEnabled  bool
	// SASLMechanism selects broker auth: "none", "plain" (username/password,
	// e.g. Google Cloud Managed Kafka service-account credentials), or
	// "aws-msk-iam".
	SASLMechanism string
	Username      string
	Password      string
}

func (c *ReaderConfig) ApplyDefaults() {
	if c.ClientID == "" {
		c.ClientID = "sei-historical-offload-consumer"
	}
	if c.StartOffset == "" {
		c.StartOffset = "first"
	}
	if c.MinBytes == 0 {
		c.MinBytes = 1
	}
	if c.MaxBytes == 0 {
		c.MaxBytes = 10 << 20
	}
	if c.MaxWait == 0 {
		c.MaxWait = 500 * time.Millisecond
	}
}

func (c *ReaderConfig) Validate() error {
	if len(c.Brokers) == 0 {
		return fmt.Errorf("kafka brokers are required")
	}
	if c.Topic == "" {
		return fmt.Errorf("kafka topic is required")
	}
	if c.GroupID == "" {
		return fmt.Errorf("kafka group id is required")
	}
	switch strings.ToLower(c.StartOffset) {
	case "", "first", "last":
	default:
		return fmt.Errorf("unsupported kafka start offset %q", c.StartOffset)
	}
	return ValidateSASL(c.saslConfig())
}

func (c ReaderConfig) saslConfig() WriterConfig {
	return WriterConfig{
		Region:        c.Region,
		TLSEnabled:    c.TLSEnabled,
		SASLMechanism: c.SASLMechanism,
		Username:      c.Username,
		Password:      c.Password,
	}
}

// NewReader builds a kafka-go Reader from cfg after applying defaults and
// validating it.
func NewReader(cfg ReaderConfig) (*kafkago.Reader, error) {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	dialer := &kafkago.Dialer{
		ClientID: cfg.ClientID,
		Timeout:  10 * time.Second,
	}
	if cfg.TLSEnabled {
		dialer.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	mech, err := NewSASLMechanism(cfg.saslConfig())
	if err != nil {
		return nil, err
	}
	dialer.SASLMechanism = mech

	start := kafkago.FirstOffset
	if strings.EqualFold(cfg.StartOffset, "last") {
		start = kafkago.LastOffset
	}

	return kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:     cfg.Brokers,
		Topic:       cfg.Topic,
		GroupID:     cfg.GroupID,
		Dialer:      dialer,
		MinBytes:    cfg.MinBytes,
		MaxBytes:    cfg.MaxBytes,
		MaxWait:     cfg.MaxWait,
		StartOffset: start,
	}), nil
}
