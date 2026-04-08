package offload

import (
	"context"
	"crypto/tls"
	"fmt"
	"strconv"
	"strings"
	"time"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/compress"
	"github.com/segmentio/kafka-go/sasl"

	dbproto "github.com/sei-protocol/sei-chain/sei-db/proto"
)

const kafkaOptionNone = "none"

type KafkaConfig struct {
	Brokers       []string
	Topic         string
	ClientID      string
	Region        string
	Async         bool
	RequiredAcks  string
	Compression   string
	BatchSize     int
	BatchTimeout  time.Duration
	BatchBytes    int
	TLSEnabled    bool
	SASLMechanism string
}

func (c *KafkaConfig) ApplyDefaults() {
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

func (c *KafkaConfig) Validate() error {
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

	switch strings.ToLower(c.SASLMechanism) {
	case "", kafkaOptionNone:
		return nil
	case "aws-msk-iam":
		if !c.TLSEnabled {
			return fmt.Errorf("kafka tls must be enabled for aws-msk-iam")
		}
		if c.Region == "" {
			return fmt.Errorf("kafka region is required for aws-msk-iam")
		}
		return nil
	default:
		return fmt.Errorf("unsupported kafka sasl mechanism %q", c.SASLMechanism)
	}
}

type kafkaStream struct {
	writer  *kafka.Writer
	durable bool
}

var _ Stream = (*kafkaStream)(nil)

func NewKafkaStream(cfg KafkaConfig) (Stream, error) {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	dialer := &kafka.Dialer{
		ClientID: cfg.ClientID,
		Timeout:  10 * time.Second,
	}
	if cfg.TLSEnabled {
		dialer.TLS = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	mechanism, err := kafkaSASLMechanism(cfg)
	if err != nil {
		return nil, err
	}
	dialer.SASLMechanism = mechanism

	requiredAcks := kafkaRequiredAcks(cfg.RequiredAcks)
	writer := &kafka.Writer{
		Addr:         kafka.TCP(cfg.Brokers...),
		Topic:        cfg.Topic,
		Balancer:     &kafka.Hash{},
		RequiredAcks: requiredAcks,
		BatchSize:    cfg.BatchSize,
		BatchTimeout: cfg.BatchTimeout,
		BatchBytes:   int64(cfg.BatchBytes),
		Async:        cfg.Async,
		Compression:  kafkaCompression(cfg.Compression),
		Transport: &kafka.Transport{
			ClientID:    cfg.ClientID,
			TLS:         dialer.TLS,
			SASL:        mechanism,
			IdleTimeout: 30 * time.Second,
			MetadataTTL: 30 * time.Second,
			DialTimeout: dialer.Timeout,
		},
	}

	return &kafkaStream{
		writer:  writer,
		durable: !cfg.Async && requiredAcks == kafka.RequireAll,
	}, nil
}

func (k *kafkaStream) Publish(ctx context.Context, entry *dbproto.ChangelogEntry) (Ack, error) {
	if entry == nil {
		return Ack{Accepted: true}, nil
	}

	payload, err := gogoproto.Marshal(entry)
	if err != nil {
		return Ack{}, fmt.Errorf("marshal changelog entry: %w", err)
	}

	key := []byte(strconv.FormatInt(entry.Version, 10))
	if err := k.writer.WriteMessages(ctx, kafka.Message{
		Key:   key,
		Value: payload,
		Time:  time.Now(),
	}); err != nil {
		return Ack{}, fmt.Errorf("publish changelog entry to kafka: %w", err)
	}

	return Ack{
		Accepted: true,
		Durable:  k.durable,
		Cursor:   string(key),
	}, nil
}

func (k *kafkaStream) Close() error {
	return k.writer.Close()
}

func kafkaRequiredAcks(requiredAcks string) kafka.RequiredAcks {
	switch strings.ToLower(requiredAcks) {
	case kafkaOptionNone:
		return kafka.RequireNone
	case "leader":
		return kafka.RequireOne
	default:
		return kafka.RequireAll
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

func kafkaSASLMechanism(cfg KafkaConfig) (sasl.Mechanism, error) {
	switch strings.ToLower(cfg.SASLMechanism) {
	case "", kafkaOptionNone:
		return nil, nil
	case "aws-msk-iam":
		return newAWSMSKIAMMechanism(cfg)
	default:
		return nil, fmt.Errorf("unsupported kafka sasl mechanism %q", cfg.SASLMechanism)
	}
}
