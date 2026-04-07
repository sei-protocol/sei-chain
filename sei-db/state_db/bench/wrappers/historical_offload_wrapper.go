package wrappers

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	scTypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload"
)

var _ DBWrapper = (*historicalOffloadWrapper)(nil)

type HistoricalOffloadConfig struct {
	Provider string
	Kafka    *KafkaHistoricalOffloadConfig
}

type KafkaHistoricalOffloadConfig struct {
	Brokers        []string
	Topic          string
	ClientID       string
	Region         string
	Async          *bool
	RequiredAcks   string
	Compression    string
	BatchSize      int
	BatchTimeoutMS int
	BatchBytes     int
	TLSEnabled     bool
	SASLMechanism  string
}

type historicalOffloadWrapper struct {
	stream  offload.Stream
	version atomic.Int64
}

func (c *HistoricalOffloadConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("historical offload config is required")
	}
	switch strings.ToLower(c.Provider) {
	case "kafka":
		if c.Kafka == nil {
			return fmt.Errorf("historical offload kafka config is required when provider is kafka")
		}
		c.Kafka.applyDefaults()
		return c.Kafka.validate()
	default:
		return fmt.Errorf("unsupported historical offload provider %q", c.Provider)
	}
}

func (c *KafkaHistoricalOffloadConfig) applyDefaults() {
	if c.ClientID == "" {
		c.ClientID = "cryptosim-historical-offload"
	}
	if c.RequiredAcks == "" {
		c.RequiredAcks = "none"
	}
	if c.Compression == "" {
		c.Compression = "snappy"
	}
	if c.BatchSize == 0 {
		c.BatchSize = 1000
	}
	if c.BatchTimeoutMS == 0 {
		c.BatchTimeoutMS = 50
	}
	if c.BatchBytes == 0 {
		c.BatchBytes = 4 << 20
	}
}

func (c *KafkaHistoricalOffloadConfig) validate() error {
	cfg := offload.KafkaConfig{
		Brokers:       c.Brokers,
		Topic:         c.Topic,
		ClientID:      c.ClientID,
		Region:        c.Region,
		Async:         c.asyncValue(),
		RequiredAcks:  c.RequiredAcks,
		Compression:   c.Compression,
		BatchSize:     c.BatchSize,
		BatchTimeout:  time.Duration(c.BatchTimeoutMS) * time.Millisecond,
		BatchBytes:    c.BatchBytes,
		TLSEnabled:    c.TLSEnabled,
		SASLMechanism: c.SASLMechanism,
	}
	return cfg.Validate()
}

func (c *KafkaHistoricalOffloadConfig) asyncValue() bool {
	return c.Async == nil || *c.Async
}

func newHistoricalOffloadStream(cfg *HistoricalOffloadConfig) (offload.Stream, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	kafkaCfg := *cfg.Kafka
	kafkaCfg.applyDefaults()
	return offload.NewKafkaStream(offload.KafkaConfig{
		Brokers:       append([]string(nil), kafkaCfg.Brokers...),
		Topic:         kafkaCfg.Topic,
		ClientID:      kafkaCfg.ClientID,
		Region:        kafkaCfg.Region,
		Async:         kafkaCfg.asyncValue(),
		RequiredAcks:  kafkaCfg.RequiredAcks,
		Compression:   kafkaCfg.Compression,
		BatchSize:     kafkaCfg.BatchSize,
		BatchTimeout:  time.Duration(kafkaCfg.BatchTimeoutMS) * time.Millisecond,
		BatchBytes:    kafkaCfg.BatchBytes,
		TLSEnabled:    kafkaCfg.TLSEnabled,
		SASLMechanism: kafkaCfg.SASLMechanism,
	})
}

func newSSHistoricalOffloadStateStore(_ context.Context, dbDir string, cfg *HistoricalOffloadConfig) (DBWrapper, error) {
	fmt.Printf("Opening historical offload stream from directory %s\n", dbDir)
	stream, err := newHistoricalOffloadStream(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create historical offload stream: %w", err)
	}
	return NewHistoricalOffloadWrapper(stream), nil
}

func NewHistoricalOffloadWrapper(stream offload.Stream) DBWrapper {
	return &historicalOffloadWrapper{stream: stream}
}

func (h *historicalOffloadWrapper) ApplyChangeSets(entry *proto.ChangelogEntry) error {
	ack, err := h.stream.Publish(context.Background(), entry)
	if err != nil {
		return err
	}
	if !ack.Accepted {
		return fmt.Errorf("historical offload publish was not acknowledged at version %d", entry.Version)
	}
	h.version.Store(entry.Version)
	return nil
}

func (h *historicalOffloadWrapper) Read(_ []byte) (data []byte, found bool, err error) {
	return nil, false, nil
}

func (h *historicalOffloadWrapper) Commit() (int64, error) {
	return h.version.Load(), nil
}

func (h *historicalOffloadWrapper) Close() error {
	var streamErr error
	if closer, ok := h.stream.(io.Closer); ok {
		streamErr = closer.Close()
	}
	return streamErr
}

func (h *historicalOffloadWrapper) Version() int64 {
	return h.version.Load()
}

func (h *historicalOffloadWrapper) LoadVersion(_ int64) error {
	return nil
}

func (h *historicalOffloadWrapper) Importer(_ int64) (scTypes.Importer, error) {
	return nil, fmt.Errorf("import not supported for historical offload wrapper")
}

func (h *historicalOffloadWrapper) GetPhaseTimer() *metrics.PhaseTimer {
	return nil
}
