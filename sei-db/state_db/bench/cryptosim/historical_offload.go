package cryptosim

import (
	"context"
	"fmt"
	"strings"
	"time"

	ssconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload"
)

type HistoricalOffloadConfig struct {
	Provider string
	Kafka    *KafkaHistoricalOffloadConfig
}

type KafkaHistoricalOffloadConfig struct {
	Brokers        []string
	Topic          string
	ClientID       string
	RequiredAcks   string
	Compression    string
	BatchTimeoutMS int
	BatchBytes     int
	TLSEnabled     bool
	SASLMechanism  string
	Username       string
	Password       string
}

func (c *CryptoSimConfig) validateHistoricalOffload() error {
	if c.HistoricalOffload == nil {
		return nil
	}

	switch strings.ToLower(c.HistoricalOffload.Provider) {
	case "", "local":
		return nil
	case "kafka":
		if c.HistoricalOffload.Kafka == nil {
			return fmt.Errorf("historical offload kafka config is required when provider is kafka")
		}
		c.HistoricalOffload.Kafka.applyDefaults()
		return c.HistoricalOffload.Kafka.validate()
	default:
		return fmt.Errorf("unsupported historical offload provider %q", c.HistoricalOffload.Provider)
	}
}

func (c *KafkaHistoricalOffloadConfig) applyDefaults() {
	if c.ClientID == "" {
		c.ClientID = "cryptosim-historical-offload"
	}
	if c.RequiredAcks == "" {
		c.RequiredAcks = "all"
	}
	if c.Compression == "" {
		c.Compression = "snappy"
	}
	if c.BatchTimeoutMS == 0 {
		c.BatchTimeoutMS = 5
	}
	if c.BatchBytes == 0 {
		c.BatchBytes = 1 << 20
	}
}

func (c *KafkaHistoricalOffloadConfig) validate() error {
	cfg := offload.KafkaConfig{
		Brokers:       c.Brokers,
		Topic:         c.Topic,
		ClientID:      c.ClientID,
		RequiredAcks:  c.RequiredAcks,
		Compression:   c.Compression,
		BatchTimeout:  time.Duration(c.BatchTimeoutMS) * time.Millisecond,
		BatchBytes:    c.BatchBytes,
		TLSEnabled:    c.TLSEnabled,
		SASLMechanism: c.SASLMechanism,
		Username:      c.Username,
		Password:      c.Password,
	}
	return cfg.Validate()
}

func configureHistoricalOffloadFactory(config *CryptoSimConfig) error {
	wrappers.SetHistoricalOffloadStreamFactory(nil)

	if config.Backend != wrappers.SSHistoricalOffload || config.HistoricalOffload == nil {
		return nil
	}

	switch strings.ToLower(config.HistoricalOffload.Provider) {
	case "", "local":
		return nil
	case "kafka":
		kafkaCfg := *config.HistoricalOffload.Kafka
		kafkaCfg.applyDefaults()
		wrappers.SetHistoricalOffloadStreamFactory(func(
			_ context.Context,
			_ string,
			_ ssconfig.StateStoreConfig,
		) (offload.Stream, error) {
			return offload.NewKafkaStream(offload.KafkaConfig{
				Brokers:       append([]string(nil), kafkaCfg.Brokers...),
				Topic:         kafkaCfg.Topic,
				ClientID:      kafkaCfg.ClientID,
				RequiredAcks:  kafkaCfg.RequiredAcks,
				Compression:   kafkaCfg.Compression,
				BatchTimeout:  time.Duration(kafkaCfg.BatchTimeoutMS) * time.Millisecond,
				BatchBytes:    kafkaCfg.BatchBytes,
				TLSEnabled:    kafkaCfg.TLSEnabled,
				SASLMechanism: kafkaCfg.SASLMechanism,
				Username:      kafkaCfg.Username,
				Password:      kafkaCfg.Password,
			})
		})
		return nil
	default:
		return fmt.Errorf("unsupported historical offload provider %q", config.HistoricalOffload.Provider)
	}
}
