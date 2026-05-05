package consumer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
	"golang.org/x/sync/errgroup"
)

// MessageSource is the subset of *kafka.Reader the consumer uses; the
// indirection lets tests drive the loop with a fake.
type MessageSource interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
}

// Consumer fans messages out to per-partition workers so cross-partition
// writes parallelize while ordering within a partition is preserved.
type Consumer struct {
	reader      MessageSource
	sink        Sink
	logf        func(format string, args ...interface{})
	workers     int
	shardBuf    int
	maxAttempts int
	baseBackoff time.Duration
	maxBackoff  time.Duration
}

const (
	defaultSinkMaxAttempts = 5
	defaultSinkBaseBackoff = 1 * time.Second
	defaultSinkMaxBackoff  = 30 * time.Second
	defaultWorkers         = 4
	defaultShardBuffer     = 128
)

// Options configures the consumer loop. Zero values pick defaults.
//
// Backpressure: the per-worker shard channel is bounded by ShardBufferSize.
// When the sink falls behind, workers stop draining their shards, the fetcher
// blocks on send, and Kafka stops being polled — propagating pressure to the
// upstream producer with no message drops.
type Options struct {
	Logf            func(format string, args ...interface{})
	SinkMaxAttempts int
	SinkBaseBackoff time.Duration
	SinkMaxBackoff  time.Duration
	// Workers sets per-partition write parallelism. Messages are sharded by
	// partition so a partition's writes stay ordered. Default 4.
	Workers int
	// ShardBufferSize bounds in-flight messages per worker. Default 128.
	ShardBufferSize int
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
	workers := opts.Workers
	if workers <= 0 {
		workers = defaultWorkers
	}
	shardBuf := opts.ShardBufferSize
	if shardBuf <= 0 {
		shardBuf = defaultShardBuffer
	}
	return &Consumer{
		reader:      reader,
		sink:        sink,
		logf:        logf,
		workers:     workers,
		shardBuf:    shardBuf,
		maxAttempts: maxAttempts,
		baseBackoff: base,
		maxBackoff:  maxBackoff,
	}
}

// Run blocks until ctx is cancelled or an unrecoverable error occurs. Offsets
// commit only after the sink persists each message (at-least-once delivery).
func (c *Consumer) Run(ctx context.Context) error {
	if c.workers == 1 {
		return c.runSerial(ctx)
	}
	return c.runParallel(ctx)
}

func (c *Consumer) runSerial(ctx context.Context) error {
	for {
		msg, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if isCancellation(err) {
				return nil
			}
			return fmt.Errorf("fetch kafka message: %w", err)
		}
		if err := c.processMessage(ctx, msg); err != nil {
			if isCancellation(err) {
				return nil
			}
			return err
		}
	}
}

func (c *Consumer) runParallel(ctx context.Context) error {
	g, gctx := errgroup.WithContext(ctx)
	shards := make([]chan kafka.Message, c.workers)
	for i := range shards {
		shards[i] = make(chan kafka.Message, c.shardBuf)
		ch := shards[i]
		g.Go(func() error { return c.workerLoop(gctx, ch) })
	}
	g.Go(func() error {
		defer func() {
			for _, ch := range shards {
				close(ch)
			}
		}()
		for {
			msg, err := c.reader.FetchMessage(gctx)
			if err != nil {
				if isCancellation(err) {
					return nil
				}
				return fmt.Errorf("fetch kafka message: %w", err)
			}
			shard := shardFor(msg.Partition, c.workers)
			select {
			case shards[shard] <- msg:
			case <-gctx.Done():
				return nil
			}
		}
	})
	if err := g.Wait(); err != nil && !isCancellation(err) {
		return err
	}
	return nil
}

func (c *Consumer) workerLoop(ctx context.Context, ch <-chan kafka.Message) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			if err := c.processMessage(ctx, msg); err != nil {
				if isCancellation(err) {
					return nil
				}
				return err
			}
		}
	}
}

func (c *Consumer) processMessage(ctx context.Context, msg kafka.Message) error {
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
		return fmt.Errorf("sink write version %d: %w", entry.Version, err)
	}
	c.logf("wrote version=%d partition=%d offset=%d in %s",
		entry.Version, msg.Partition, msg.Offset, time.Since(start))
	if err := c.reader.CommitMessages(ctx, msg); err != nil {
		return fmt.Errorf("commit kafka offset %d: %w", msg.Offset, err)
	}
	return nil
}

// writeWithRetry calls sink.Write with bounded exponential backoff.
func (c *Consumer) writeWithRetry(ctx context.Context, rec Record) error {
	backoff := c.baseBackoff
	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		err := c.sink.Write(ctx, rec)
		if err == nil {
			return nil
		}
		lastErr = err
		if isCancellation(err) {
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

func shardFor(partition, workers int) int {
	if partition < 0 {
		partition = -partition
	}
	return partition % workers
}

func isCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
