package offload

import (
	"context"
	"fmt"
	"strconv"
	"time"

	gogoproto "github.com/gogo/protobuf/proto"
	kafkago "github.com/segmentio/kafka-go"

	dbproto "github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/queue/kafka"
)

// KafkaConfig aliases the shared Kafka writer configuration so existing
// callers keep building it with keyed literals.
type KafkaConfig = kafka.WriterConfig

type kafkaStream struct {
	writer  *kafkago.Writer
	durable bool
}

var _ Stream = (*kafkaStream)(nil)

func NewKafkaStream(cfg KafkaConfig) (Stream, error) {
	cfg.ApplyDefaults()
	writer, err := kafka.NewWriter(cfg)
	if err != nil {
		return nil, err
	}
	return &kafkaStream{
		writer:  writer,
		durable: !cfg.Async && writer.RequiredAcks == kafkago.RequireAll,
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
	if err := k.writer.WriteMessages(ctx, kafkago.Message{
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
