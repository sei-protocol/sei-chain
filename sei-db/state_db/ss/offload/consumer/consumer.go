package consumer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
)

// MessageSource is the subset of *kafka.Reader the consumer uses. Extracting
// it lets tests drive the loop with a fake without a running Kafka.
type MessageSource interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
}

// Consumer pulls messages from a MessageSource, decodes them, writes to a Sink,
// and commits offsets. It is single-threaded by design: ordering per partition
// is required so the CockroachDB primary key (store_name, key, version DESC)
// reflects producer order.
type Consumer struct {
	reader MessageSource
	sink   Sink
	logf   func(format string, args ...interface{})
}

// Options are optional hooks for the consumer loop.
type Options struct {
	Logf func(format string, args ...interface{})
}

func New(reader MessageSource, sink Sink, opts Options) *Consumer {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...interface{}) {}
	}
	return &Consumer{reader: reader, sink: sink, logf: logf}
}

// Run blocks until ctx is cancelled or an unrecoverable error occurs.
// It commits offsets only after the sink has persisted each message, so
// at-least-once delivery is preserved across restarts.
func (c *Consumer) Run(ctx context.Context) error {
	for {
		msg, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return fmt.Errorf("fetch kafka message: %w", err)
		}

		entry, err := DecodeEntry(msg.Value)
		if err != nil {
			return fmt.Errorf("decode message at offset %d: %w", msg.Offset, err)
		}

		rec := Record{
			Topic:     msg.Topic,
			Partition: msg.Partition,
			Offset:    msg.Offset,
			Entry:     entry,
		}
		start := time.Now()
		if err := c.sink.Write(ctx, rec); err != nil {
			return fmt.Errorf("sink write version %d: %w", entry.Version, err)
		}
		c.logf("wrote version=%d partition=%d offset=%d in %s",
			entry.Version, msg.Partition, msg.Offset, time.Since(start))

		if err := c.reader.CommitMessages(ctx, msg); err != nil {
			return fmt.Errorf("commit kafka offset %d: %w", msg.Offset, err)
		}
	}
}
