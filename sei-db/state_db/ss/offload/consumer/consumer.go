package consumer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
	"golang.org/x/sync/errgroup"
)

// MessageSource is the subset of *kafka.Reader used by the loop.
type MessageSource interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
}

// Messages are sharded by partition: cross-partition writes parallelize while
// ordering within a partition is preserved.
type Consumer struct {
	reader      MessageSource
	sink        Sink
	logf        func(format string, args ...interface{})
	workers     int
	shardBuf    int
	batchSize   int
	batchWait   time.Duration
	maxAttempts int
	baseBackoff time.Duration
	maxBackoff  time.Duration
	metrics     *consumerMetrics
}

const (
	defaultSinkMaxAttempts = 5
	defaultSinkBaseBackoff = 1 * time.Second
	defaultSinkMaxBackoff  = 30 * time.Second
	defaultWorkers         = 16
	defaultShardBuffer     = 128
	defaultBatchSize       = 16
	defaultBatchMaxWait    = 10 * time.Millisecond
)

// Backpressure: when the sink falls behind, ShardBufferSize fills, the fetcher
// blocks, and Kafka stops being polled. Zero values pick defaults.
type Options struct {
	Logf            func(format string, args ...interface{})
	SinkMaxAttempts int
	SinkBaseBackoff time.Duration
	SinkMaxBackoff  time.Duration
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
	batchSize := opts.MaxBatchRecords
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	batchWait := opts.BatchMaxWait
	if batchWait <= 0 {
		batchWait = defaultBatchMaxWait
	}
	return &Consumer{
		reader:      reader,
		sink:        sink,
		logf:        logf,
		workers:     workers,
		shardBuf:    shardBuf,
		batchSize:   batchSize,
		batchWait:   batchWait,
		maxAttempts: maxAttempts,
		baseBackoff: base,
		maxBackoff:  maxBackoff,
		metrics:     newConsumerMetrics(),
	}
}

// Run commits offsets only after the sink persists each message.
func (c *Consumer) Run(ctx context.Context) error {
	return c.runParallel(ctx)
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

func (c *Consumer) collectBatch(ctx context.Context, ch <-chan kafka.Message, first kafka.Message) ([]kafka.Message, bool) {
	msgs := make([]kafka.Message, 1, c.batchSize)
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

func (c *Consumer) processBatch(ctx context.Context, msgs []kafka.Message) error {
	records := make([]Record, 0, len(msgs))
	var firstVersion, lastVersion int64
	for i, msg := range msgs {
		entry, err := DecodeEntry(msg.Value)
		if err != nil {
			return fmt.Errorf("decode message at offset %d: %w", msg.Offset, err)
		}
		if i == 0 {
			firstVersion = entry.Version
		}
		lastVersion = entry.Version
		records = append(records, Record{
			Topic:     msg.Topic,
			Partition: msg.Partition,
			Offset:    msg.Offset,
			Entry:     entry,
		})
	}
	start := time.Now()
	if err := c.writeBatchWithRetry(ctx, records); err != nil {
		return fmt.Errorf("sink write batch first_version=%d last_version=%d: %w",
			firstVersion, lastVersion, err)
	}
	writeLatency := time.Since(start)
	c.logf("wrote records=%d first_version=%d last_version=%d in %s",
		len(records), firstVersion, lastVersion, writeLatency)
	if err := c.reader.CommitMessages(ctx, msgs...); err != nil {
		return fmt.Errorf("commit kafka offsets: %w", err)
	}
	c.metrics.recordBatch(ctx, int64(len(records)), batchLag(msgs), writeLatency)
	return nil
}

// batchLag reports how far behind the partition head the batch left the
// consumer: the max over messages of (high watermark - offset - 1). Returns -1
// when no message carries a watermark so the gauge is left untouched.
func batchLag(msgs []kafka.Message) int64 {
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

func (c *Consumer) writeBatchWithRetry(ctx context.Context, records []Record) error {
	backoff := c.baseBackoff
	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		err := c.writeRecords(ctx, records)
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
		if err := sleepWithContext(ctx, backoff); err != nil {
			return err
		}
		backoff *= 2
		if backoff > c.maxBackoff {
			backoff = c.maxBackoff
		}
	}
	return fmt.Errorf("sink write failed after %d attempts: %w", c.maxAttempts, lastErr)
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

func (c *Consumer) writeRecords(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	if sink, ok := c.sink.(BatchSink); ok {
		return sink.WriteBatch(ctx, records)
	}
	for _, rec := range records {
		if err := c.sink.Write(ctx, rec); err != nil {
			return err
		}
	}
	return nil
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
