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
	reader      MessageSource
	sink        Sink
	logf        func(format string, args ...interface{})
	maxAttempts int
	baseBackoff time.Duration
	maxBackoff  time.Duration
}

// Default retry knobs for sink writes. Total wait at defaults ≈ 1+2+4+8 = 15s
// before giving up and letting the process supervisor restart us.
const (
	defaultSinkMaxAttempts = 5
	defaultSinkBaseBackoff = 1 * time.Second
	defaultSinkMaxBackoff  = 30 * time.Second
)

// Options are optional hooks for the consumer loop. Zero values pick defaults.
type Options struct {
	Logf func(format string, args ...interface{})
	// SinkMaxAttempts caps total sink.Write attempts per message (>=1).
	SinkMaxAttempts int
	// SinkBaseBackoff is the initial backoff between retries; doubles each retry.
	SinkBaseBackoff time.Duration
	// SinkMaxBackoff caps the per-retry backoff.
	SinkMaxBackoff time.Duration
}

func New(reader MessageSource, sink Sink, opts Options) *Consumer {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...interface{}) {}
	}
	maxAttempts := opts.SinkMaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultSinkMaxAttempts
	}
	base := opts.SinkBaseBackoff
	if base <= 0 {
		base = defaultSinkBaseBackoff
	}
	maxBackoff := opts.SinkMaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = defaultSinkMaxBackoff
	}
	return &Consumer{
		reader:      reader,
		sink:        sink,
		logf:        logf,
		maxAttempts: maxAttempts,
		baseBackoff: base,
		maxBackoff:  maxBackoff,
	}
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
		if err := c.writeWithRetry(ctx, rec); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return fmt.Errorf("sink write version %d: %w", entry.Version, err)
		}
		c.logf("wrote version=%d partition=%d offset=%d in %s",
			entry.Version, msg.Partition, msg.Offset, time.Since(start))

		if err := c.reader.CommitMessages(ctx, msg); err != nil {
			return fmt.Errorf("commit kafka offset %d: %w", msg.Offset, err)
		}
	}
}

// writeWithRetry calls sink.Write with bounded exponential backoff. It returns
// the underlying error after the final attempt, or ctx.Err() if cancelled
// while sleeping between retries.
func (c *Consumer) writeWithRetry(ctx context.Context, rec Record) error {
	backoff := c.baseBackoff
	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		err := c.sink.Write(ctx, rec)
		if err == nil {
			return nil
		}
		lastErr = err
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if attempt == c.maxAttempts {
			break
		}
		c.logf("sink write attempt %d/%d failed: %v; retrying in %s",
			attempt, c.maxAttempts, err, backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}
		backoff *= 2
		if backoff > c.maxBackoff {
			backoff = c.maxBackoff
		}
	}
	return fmt.Errorf("sink write failed after %d attempts: %w", c.maxAttempts, lastErr)
}
