package kafka

import (
	"context"
	"errors"
	"fmt"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"golang.org/x/sync/errgroup"
)

// MessageSource is the subset of *kafka.Reader used by the loop.
type MessageSource interface {
	FetchMessage(ctx context.Context) (kafkago.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafkago.Message) error
}

// Sink persists batches of raw Kafka messages. Implementations own payload
// decoding; the consume loop stays payload-agnostic.
type Sink interface {
	WriteBatch(ctx context.Context, msgs []kafkago.Message) error
	Close() error
}

// Messages are sharded by partition: cross-partition writes parallelize while
// ordering within a partition is preserved.
type Consumer struct {
	reader    MessageSource
	sink      Sink
	logf      func(format string, args ...interface{})
	workers   int
	shardBuf  int
	batchSize int
	batchWait time.Duration
	metrics   *consumerMetrics
}

const (
	sinkMaxAttempts = 5
	sinkBaseBackoff = 1 * time.Second
	sinkMaxBackoff  = 30 * time.Second

	defaultWorkers      = 16
	defaultShardBuffer  = 128
	defaultBatchSize    = 16
	defaultBatchMaxWait = 10 * time.Millisecond
)

// Backpressure: when the sink falls behind, ShardBufferSize fills, the fetcher
// blocks, and Kafka stops being polled. Zero values pick defaults.
type Options struct {
	Logf            func(format string, args ...interface{})
	Workers         int
	ShardBufferSize int
	MaxBatchRecords int
	BatchMaxWait    time.Duration
}

func New(reader MessageSource, sink Sink, opts Options) *Consumer {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...interface{}) {}
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = defaultWorkers
	}
	shardBuf := opts.ShardBufferSize
	if shardBuf <= 0 {
		shardBuf = defaultShardBuffer
	}
	batchSize := opts.MaxBatchRecords
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	batchWait := opts.BatchMaxWait
	if batchWait <= 0 {
		batchWait = defaultBatchMaxWait
	}
	return &Consumer{
		reader:    reader,
		sink:      sink,
		logf:      logf,
		workers:   workers,
		shardBuf:  shardBuf,
		batchSize: batchSize,
		batchWait: batchWait,
		metrics:   newConsumerMetrics(),
	}
}

// Run commits offsets only after the sink persists each message.
func (c *Consumer) Run(ctx context.Context) error {
	g, gctx := errgroup.WithContext(ctx)
	shards := make([]chan kafkago.Message, c.workers)
	for i := range shards {
		shards[i] = make(chan kafkago.Message, c.shardBuf)
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

func (c *Consumer) workerLoop(ctx context.Context, ch <-chan kafkago.Message) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			msgs, ok := c.collectBatch(ctx, ch, msg)
			if !ok {
				return nil
			}
			if err := c.processBatch(ctx, msgs); err != nil {
				if isCancellation(err) {
					return nil
				}
				return err
			}
		}
	}
}

func (c *Consumer) collectBatch(ctx context.Context, ch <-chan kafkago.Message, first kafkago.Message) ([]kafkago.Message, bool) {
	msgs := make([]kafkago.Message, 1, c.batchSize)
	msgs[0] = first
	if c.batchSize <= 1 {
		return msgs, true
	}

drainBuffered:
	for len(msgs) < c.batchSize {
		select {
		case <-ctx.Done():
			return nil, false
		case msg, ok := <-ch:
			if !ok {
				return msgs, true
			}
			msgs = append(msgs, msg)
		default:
			break drainBuffered
		}
	}
	if len(msgs) == c.batchSize {
		return msgs, true
	}

	timer := time.NewTimer(c.batchWait)
	defer timer.Stop()
	for len(msgs) < c.batchSize {
		select {
		case <-ctx.Done():
			return nil, false
		case msg, ok := <-ch:
			if !ok {
				return msgs, true
			}
			msgs = append(msgs, msg)
		case <-timer.C:
			return msgs, true
		}
	}
	return msgs, true
}

func (c *Consumer) processBatch(ctx context.Context, msgs []kafkago.Message) error {
	firstOffset := msgs[0].Offset
	lastOffset := msgs[len(msgs)-1].Offset
	start := time.Now()
	if err := c.writeBatchWithRetry(ctx, msgs); err != nil {
		return fmt.Errorf("sink write batch first_offset=%d last_offset=%d: %w",
			firstOffset, lastOffset, err)
	}
	writeLatency := time.Since(start)
	c.logf("wrote messages=%d first_offset=%d last_offset=%d in %s",
		len(msgs), firstOffset, lastOffset, writeLatency)
	if err := c.reader.CommitMessages(ctx, msgs...); err != nil {
		return fmt.Errorf("commit kafka offsets: %w", err)
	}
	c.metrics.recordBatch(ctx, int64(len(msgs)), batchLag(msgs), writeLatency)
	return nil
}

// batchLag reports how far behind the partition head the batch left the
// consumer: the max over messages of (high watermark - offset - 1). Returns -1
// when no message carries a watermark so the gauge is left untouched.
func batchLag(msgs []kafkago.Message) int64 {
	lag := int64(-1)
	for _, msg := range msgs {
		if msg.HighWaterMark <= 0 {
			continue
		}
		if l := msg.HighWaterMark - msg.Offset - 1; l > lag {
			lag = l
		}
	}
	return lag
}

func (c *Consumer) writeBatchWithRetry(ctx context.Context, msgs []kafkago.Message) error {
	backoff := sinkBaseBackoff
	var lastErr error
	for attempt := 1; attempt <= sinkMaxAttempts; attempt++ {
		err := c.sink.WriteBatch(ctx, msgs)
		if err == nil {
			return nil
		}
		lastErr = err
		if isCancellation(err) {
			return err
		}
		if attempt == sinkMaxAttempts {
			break
		}
		c.logf("sink write attempt %d/%d failed: %v; retrying in %s",
			attempt, sinkMaxAttempts, err, backoff)
		if err := sleepWithContext(ctx, backoff); err != nil {
			return err
		}
		backoff *= 2
		if backoff > sinkMaxBackoff {
			backoff = sinkMaxBackoff
		}
	}
	return fmt.Errorf("sink write failed after %d attempts: %w", sinkMaxAttempts, lastErr)
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
