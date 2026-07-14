package seiwal

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"
)

var _ WAL[[]byte] = (*serializingWAL[[]byte])(nil)

// serAppend carries a framed-payload producer to the serializer goroutine. The closure captures the typed
// item so this message type stays non-generic — T never enters the channel's dynamic type, which keeps the
// serializer loop's type switch free of type parameters.
type serAppend struct {
	index     uint64
	serialize func() ([]byte, error)
}

// serFlush asks the serializer goroutine to flush the inner WAL, signaling done when durable.
type serFlush struct {
	done chan error
}

// serBounds asks the serializer goroutine to report the inner WAL's stored index range.
type serBounds struct {
	reply chan serBoundsResult
}

// The index range (and any error) reported by the inner WAL's Bounds.
type serBoundsResult struct {
	ok    bool
	first uint64
	last  uint64
	err   error
}

// serPrune asks the serializer goroutine to prune the inner WAL below `through`.
type serPrune struct {
	through uint64
}

// serIterator asks the serializer goroutine to create an inner iterator, ordered after every prior append.
type serIterator struct {
	startIndex uint64
	reply      chan serIteratorResult
}

// The inner iterator (or an error) produced in response to a serIterator request.
type serIteratorResult struct {
	it  Iterator[[]byte]
	err error
}

// serClose asks the serializer goroutine to close the inner WAL and shut down, signaling done when closed.
type serClose struct {
	done chan error
}

// serializingWAL is a WAL[T] that serializes each payload to []byte on a background goroutine.
type serializingWAL[T any] struct {
	// The inner byte-oriented WAL that framed records are delegated to.
	inner WAL[[]byte]

	// Serializes a payload to bytes; runs on the serializer goroutine.
	serialize func(T) ([]byte, error)
	// Deserializes stored bytes back to a payload; runs inline in the iterator.
	deserialize func([]byte) (T, error)

	// The measurement option tagging this instance's metrics with its name. Read-only after construction.
	metricAttrs metric.MeasurementOption

	// Caller entry points funnel through serializerChan as a single ordered stream to the serializer.
	serializerChan chan any

	// The hard-stop context the serializer watches. Cancelled by fail() with the fatal error as its cause,
	// and by Close() (with a nil cause) once everything has drained. The cause carries the fatal error to
	// callers, so no separate error field is needed.
	ctx context.Context
	// Cancels ctx, tearing down the serializer goroutine, recording the fatal error (or nil) as the cause.
	cancel context.CancelCauseFunc

	// A child of ctx that the serializerChan producers watch, cancelled once the serializer stops reading so
	// an in-flight or future push aborts rather than deadlocking.
	senderCtx context.Context
	// Cancels senderCtx.
	senderCancel context.CancelCauseFunc

	// Tracks the serializer and queue-depth sampler goroutines so Close() can wait for them to exit.
	wg sync.WaitGroup

	// Closed by Close() to stop the queue-depth sampler goroutine.
	samplerStop chan struct{}

	// Guarantees the Close() shutdown sequence runs at most once.
	closeOnce sync.Once

	// Set by Close() so subsequent scheduling calls fail fast. Plain: calling any method after Close is a
	// contract violation, so this need not be atomic.
	closed bool
}

func newSerializingWAL[T any](
	config *Config,
	inner WAL[[]byte],
	serialize func(T) ([]byte, error),
	deserialize func([]byte) (T, error),
) *serializingWAL[T] {
	ctx, cancel := context.WithCancelCause(context.Background())
	senderCtx, senderCancel := context.WithCancelCause(ctx)

	s := &serializingWAL[T]{
		inner:          inner,
		serialize:      serialize,
		deserialize:    deserialize,
		metricAttrs:    walNameAttr(config.Name),
		serializerChan: make(chan any, config.SerializerBufferSize),
		ctx:            ctx,
		cancel:         cancel,
		senderCtx:      senderCtx,
		senderCancel:   senderCancel,
		samplerStop:    make(chan struct{}),
	}

	s.wg.Add(1)
	go s.serializerLoop()

	if config.MetricsSampleInterval > 0 {
		s.wg.Add(1)
		go s.sampleQueueDepth(config.Name, config.MetricsSampleInterval)
	}

	return s
}

// sampleQueueDepth periodically records the serializer channel's buffered depth until Close stops it
// (samplerStop) or a fatal shutdown cancels ctx.
func (s *serializingWAL[T]) sampleQueueDepth(name string, interval time.Duration) {
	defer s.wg.Done()
	attrs := queueDepthAttrs(name, "serializer")
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.samplerStop:
			return
		case <-ticker.C:
			walQueueDepth.Record(s.ctx, int64(len(s.serializerChan)), attrs)
		}
	}
}

// Append schedules a payload to be serialized and appended at the given index.
func (s *serializingWAL[T]) Append(index uint64, data T) error {
	if s.closed {
		return fmt.Errorf("WAL is closed")
	}
	req := serAppend{
		index:     index,
		serialize: func() ([]byte, error) { return s.serialize(data) },
	}
	if err := s.submit(req); err != nil {
		return fmt.Errorf("failed to schedule append for index %d: %w", index, err)
	}
	return nil
}

// Flush blocks until all previously scheduled appends are durable.
func (s *serializingWAL[T]) Flush() error {
	done := make(chan error, 1)
	if err := s.submit(serFlush{done: done}); err != nil {
		return fmt.Errorf("failed to schedule flush: %w", err)
	}
	select {
	case err := <-done:
		return err // already wrapped by the inner WAL, or nil on success
	case <-s.ctx.Done():
		if err := s.asyncError(); err != nil {
			return fmt.Errorf("flush aborted: %w", err)
		}
		return fmt.Errorf("flush aborted: %w", s.ctx.Err())
	}
}

// Bounds reports the range of record indices stored in the WAL.
func (s *serializingWAL[T]) Bounds() (bool, uint64, uint64, error) {
	reply := make(chan serBoundsResult, 1)
	if err := s.submit(serBounds{reply: reply}); err != nil {
		return false, 0, 0, fmt.Errorf("failed to schedule bounds query: %w", err)
	}
	select {
	case r := <-reply:
		if r.err != nil {
			return false, 0, 0, fmt.Errorf("bounds query failed: %w", r.err)
		}
		return r.ok, r.first, r.last, nil
	case <-s.ctx.Done():
		if err := s.asyncError(); err != nil {
			return false, 0, 0, fmt.Errorf("bounds query aborted: %w", err)
		}
		return false, 0, 0, fmt.Errorf("bounds query aborted: %w", s.ctx.Err())
	}
}

// PruneBefore schedules removal of whole inner files below lowestIndexToKeep. It does not block on completion.
func (s *serializingWAL[T]) PruneBefore(lowestIndexToKeep uint64) error {
	if err := s.submit(serPrune{through: lowestIndexToKeep}); err != nil {
		return fmt.Errorf("failed to schedule prune below index %d: %w", lowestIndexToKeep, err)
	}
	return nil
}

// Iterator returns an iterator over the WAL starting at startIndex. Construction is ordered on the serializer
// goroutine after every prior append, so the iterator observes all previously scheduled appends.
func (s *serializingWAL[T]) Iterator(startIndex uint64) (Iterator[T], error) {
	reply := make(chan serIteratorResult, 1)
	if err := s.submit(serIterator{startIndex: startIndex, reply: reply}); err != nil {
		return nil, fmt.Errorf("failed to schedule iterator creation: %w", err)
	}
	select {
	case r := <-reply:
		if r.err != nil {
			return nil, fmt.Errorf("failed to create iterator: %w", r.err)
		}
		return &serializingIterator[T]{inner: r.it, deserialize: s.deserialize}, nil
	case <-s.ctx.Done():
		if err := s.asyncError(); err != nil {
			return nil, fmt.Errorf("iterator creation aborted: %w", err)
		}
		return nil, fmt.Errorf("iterator creation aborted: %w", s.ctx.Err())
	}
}

// Close flushes pending appends, closes the inner WAL, and releases resources.
func (s *serializingWAL[T]) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		s.closed = true
		close(s.samplerStop) // stop the queue-depth sampler before waiting for goroutines
		done := make(chan error, 1)
		if err := s.submit(serClose{done: done}); err == nil {
			select {
			case closeErr = <-done:
			case <-s.ctx.Done():
			}
		}
		s.wg.Wait()
		s.cancel(nil) // a clean close carries no fatal cause; a prior fail() already recorded one
	})
	if err := s.asyncError(); err != nil {
		return fmt.Errorf("WAL closed with error: %w", err)
	}
	return closeErr // already wrapped by the inner WAL, or nil on a clean close
}

// submit enqueues a message onto the serializer's input channel, aborting if the WAL is shutting down or has
// failed.
func (s *serializingWAL[T]) submit(msg any) error {
	// Prioritize shutdown: if the sender context is already done, never race the send case of the select
	// below, which could otherwise enqueue onto a stopped serializer's buffer and silently drop the record.
	select {
	case <-s.senderCtx.Done():
		return s.senderErr()
	default:
	}
	select {
	case s.serializerChan <- msg:
		return nil
	case <-s.senderCtx.Done():
		return s.senderErr()
	}
}

// senderErr reports why a submit was aborted: the fatal cause if the WAL bricked, or a plain closed error if
// it was shut down normally.
func (s *serializingWAL[T]) senderErr() error {
	if cause := context.Cause(s.senderCtx); cause != nil && cause != context.Canceled {
		return fmt.Errorf("WAL failed: %w", cause)
	}
	return fmt.Errorf("WAL is closed")
}

// serializerLoop serializes each append's payload and delegates it to the inner WAL, handling control
// messages (flush, bounds, prune, iterator, close) in FIFO order relative to appends so they observe a
// consistent view. Runs on its own goroutine until close or a fatal error.
func (s *serializingWAL[T]) serializerLoop() {
	defer s.wg.Done()
	for {
		var msg any
		select {
		case <-s.ctx.Done():
			return
		case msg = <-s.serializerChan:
		}

		switch m := msg.(type) {
		case serAppend:
			start := time.Now()
			data, err := m.serialize()
			if err != nil {
				walSerializeErrors.Add(s.ctx, 1, s.metricAttrs)
				s.fail(fmt.Errorf("failed to serialize record for index %d: %w", m.index, err))
				return
			}
			walSerializeDuration.Record(s.ctx, time.Since(start).Seconds(), s.metricAttrs)
			walSerializedBytes.Add(s.ctx, int64(len(data)), s.metricAttrs)
			if err := s.inner.Append(m.index, data); err != nil {
				s.fail(fmt.Errorf("failed to append record for index %d: %w", m.index, err))
				return
			}
		case serFlush:
			err := s.inner.Flush()
			m.done <- err
			if err != nil {
				s.fail(fmt.Errorf("failed to flush: %w", err))
				return
			}
		case serBounds:
			ok, first, last, err := s.inner.Bounds()
			m.reply <- serBoundsResult{ok: ok, first: first, last: last, err: err}
			if err != nil {
				s.fail(fmt.Errorf("bounds query failed: %w", err))
				return
			}
		case serPrune:
			if err := s.inner.PruneBefore(m.through); err != nil {
				s.fail(fmt.Errorf("failed to prune below index %d: %w", m.through, err))
				return
			}
		case serIterator:
			it, err := s.inner.Iterator(m.startIndex)
			m.reply <- serIteratorResult{it: it, err: err}
			if err != nil {
				s.fail(fmt.Errorf("failed to create iterator: %w", err))
				return
			}
		case serClose:
			m.done <- s.inner.Close()
			// FIFO guarantees every prior append has been delegated. Forbid further pushes so any
			// racing/future schedule aborts instead of deadlocking against the now-exiting serializer.
			s.senderCancel(nil) // normal shutdown, not a failure
			return
		}
	}
}

// fail records the first fatal background error and triggers shutdown of the pipeline. The error is recorded
// as the cancellation cause of ctx, so callers observe it via asyncError / context.Cause.
func (s *serializingWAL[T]) fail(err error) {
	s.cancel(err) // the first cancel wins, so the first fatal error is the one retained
	if cerr := s.inner.Close(); cerr != nil {
		logger.Error("failed to close inner WAL after fatal error", "err", cerr)
	}
	logger.Error("serializing WAL encountered a fatal error", "err", err)
}

// asyncError returns the first fatal background error, or nil if the WAL is healthy or was closed normally.
func (s *serializingWAL[T]) asyncError() error {
	if cause := context.Cause(s.ctx); cause != nil && cause != context.Canceled {
		return cause
	}
	return nil
}

var _ Iterator[[]byte] = (*serializingIterator[[]byte])(nil)

// serializingIterator adapts an inner byte iterator to a typed iterator by running deserialize inline in Next.
// Like the inner iterator, it is single-consumer and not safe for concurrent use (see the Iterator
// concurrency contract).
type serializingIterator[T any] struct {
	inner       Iterator[[]byte]
	deserialize func([]byte) (T, error)
	index       uint64
	entry       T
}

func (it *serializingIterator[T]) Next() (bool, error) {
	ok, err := it.inner.Next()
	if err != nil || !ok {
		var zero T
		it.entry = zero
		return false, err
	}
	index, data := it.inner.Entry()
	value, err := it.deserialize(data)
	if err != nil {
		var zero T
		it.entry = zero
		return false, fmt.Errorf("failed to deserialize record at index %d: %w", index, err)
	}
	it.index = index
	it.entry = value
	return true, nil
}

func (it *serializingIterator[T]) Entry() (uint64, T) {
	return it.index, it.entry
}

func (it *serializingIterator[T]) Close() error {
	return it.inner.Close()
}
