package consumer

import (
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/segmentio/kafka-go"

	dbproto "github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload"
)

// TLS/SASL must match the producer cluster. Commits are synchronous
// (kafka-go's zero CommitInterval) so offsets only advance after the sink
// persists each entry.
type KafkaReaderConfig struct {
	Brokers       []string
	Topic         string
	GroupID       string
	ClientID      string
	Region        string
	StartOffset   string // "first" or "last"; defaults to "first"
	MinBytes      int
	MaxBytes      int
	MaxWait       time.Duration
	TLSEnabled    bool
	SASLMechanism string
}

func (c *KafkaReaderConfig) ApplyDefaults() {
	if c.ClientID == "" {
		c.ClientID = "cryptosim-historical-offload-consumer"
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

func (c *KafkaReaderConfig) Validate() error {
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
	// Mirror the producer-side SASL checks so a misconfigured consumer fails at
	// load time instead of with an obscure handshake error on first fetch.
	switch strings.ToLower(c.SASLMechanism) {
	case "", "none":
	case "aws-msk-iam":
		if !c.TLSEnabled {
			return fmt.Errorf("kafka tls must be enabled for aws-msk-iam")
		}
		if c.Region == "" {
			return fmt.Errorf("kafka region is required for aws-msk-iam")
		}
	default:
		return fmt.Errorf("unsupported kafka sasl mechanism %q", c.SASLMechanism)
	}
	return nil
}

func NewKafkaReader(cfg KafkaReaderConfig) (*kafka.Reader, error) {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	dialer := &kafka.Dialer{
		ClientID: cfg.ClientID,
		Timeout:  10 * time.Second,
	}
	if cfg.TLSEnabled {
		dialer.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	mech, err := offload.NewSASLMechanism(offload.KafkaConfig{
		Region:        cfg.Region,
		TLSEnabled:    cfg.TLSEnabled,
		SASLMechanism: cfg.SASLMechanism,
	})
	if err != nil {
		return nil, err
	}
	dialer.SASLMechanism = mech

	start := kafka.FirstOffset
	if strings.EqualFold(cfg.StartOffset, "last") {
		start = kafka.LastOffset
	}

	return kafka.NewReader(kafka.ReaderConfig{
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

func DecodeEntry(payload []byte) (*dbproto.ChangelogEntry, error) {
	entry := &dbproto.ChangelogEntry{}
	if err := gogoproto.Unmarshal(payload, entry); err != nil {
		return nil, fmt.Errorf("decode changelog entry: %w", err)
	}
	return entry, nil
}
