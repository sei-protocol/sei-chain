package seiwal

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
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

	// Serialize a payload to bytes (run on the serializer goroutine) and deserialize it back (run inline in
	// the iterator).
	serialize   func(T) ([]byte, error)
	deserialize func([]byte) (T, error)

	// Caller entry points funnel through serializerChan as a single ordered stream to the serializer.
	serializerChan chan any

	// The hard-stop context the serializer watches. Cancelled by fail() on a fatal error and by Close() once
	// everything has drained.
	ctx context.Context
	// Cancels ctx, tearing down the serializer goroutine.
	cancel context.CancelFunc

	// A child of ctx that the serializerChan producers watch, cancelled once the serializer stops reading so
	// an in-flight or future push aborts rather than deadlocking.
	senderCtx context.Context
	// Cancels senderCtx.
	senderCancel context.CancelFunc

	// Tracks the serializer goroutine so Close() can wait for it to exit.
	wg sync.WaitGroup

	// Guarantees the Close() shutdown sequence runs at most once.
	closeOnce sync.Once

	// Set by Close() so subsequent scheduling calls fail fast.
	closed atomic.Bool

	// The first unrecoverable background-goroutine error, surfaced to the caller by Close().
	asyncErr atomic.Pointer[error]
}

// NewGenericWAL opens a WAL over payloads of type T that does serialization on a background goroutine.
func NewGenericWAL[T any](
	config *Config,
	serialize func(T) ([]byte, error),
	deserialize func([]byte) (T, error),
) (WAL[T], error) {
	inner, err := NewWAL(config)
	if err != nil {
		return nil, fmt.Errorf("failed to open inner WAL: %w", err)
	}
	return newSerializingWAL(config, inner, serialize, deserialize), nil
}

// NewGenericWALWithRollback is like NewGenericWAL but first rolls the inner WAL back so it contains no record
// with an index greater than rollbackIndex.
func NewGenericWALWithRollback[T any](
	config *Config,
	rollbackIndex uint64,
	serialize func(T) ([]byte, error),
	deserialize func([]byte) (T, error),
) (WAL[T], error) {
	inner, err := NewWALWithRollback(config, rollbackIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to open inner WAL: %w", err)
	}
	return newSerializingWAL(config, inner, serialize, deserialize), nil
}

func newSerializingWAL[T any](
	config *Config,
	inner WAL[[]byte],
	serialize func(T) ([]byte, error),
	deserialize func([]byte) (T, error),
) *serializingWAL[T] {
	ctx, cancel := context.WithCancel(context.Background())
	senderCtx, senderCancel := context.WithCancel(ctx)

	s := &serializingWAL[T]{
		inner:          inner,
		serialize:      serialize,
		deserialize:    deserialize,
		serializerChan: make(chan any, config.SerializerBufferSize),
		ctx:            ctx,
		cancel:         cancel,
		senderCtx:      senderCtx,
		senderCancel:   senderCancel,
	}

	s.wg.Add(1)
	go s.serializerLoop()

	return s
}

// Append schedules a payload to be serialized and appended at the given index.
func (s *serializingWAL[T]) Append(index uint64, data T) error {
	if s.closed.Load() {
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

// Prune schedules removal of whole inner files below lowestIndexToKeep. It does not block on completion.
func (s *serializingWAL[T]) Prune(lowestIndexToKeep uint64) error {
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
		s.closed.Store(true)
		done := make(chan error, 1)
		if err := s.submit(serClose{done: done}); err == nil {
			select {
			case closeErr = <-done:
			case <-s.ctx.Done():
			}
		}
		s.wg.Wait()
		s.cancel()
	})
	if err := s.asyncError(); err != nil {
		return fmt.Errorf("WAL closed with error: %w", err)
	}
	return closeErr // already wrapped by the inner WAL, or nil on a clean close
}

// submit enqueues a message onto the serializer's input channel, aborting if the WAL is shutting down or has
// failed.
func (s *serializingWAL[T]) submit(msg any) error {
	select {
	case s.serializerChan <- msg:
		return nil
	case <-s.senderCtx.Done():
		if err := s.asyncError(); err != nil {
			return fmt.Errorf("WAL failed: %w", err)
		}
		return fmt.Errorf("WAL is closed")
	}
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
			data, err := m.serialize()
			if err != nil {
				s.fail(fmt.Errorf("failed to serialize record for index %d: %w", m.index, err))
				return
			}
			if err := s.inner.Append(m.index, data); err != nil {
				s.fail(fmt.Errorf("failed to append record for index %d: %w", m.index, err))
				return
			}
		case serFlush:
			m.done <- s.inner.Flush()
		case serBounds:
			ok, first, last, err := s.inner.Bounds()
			m.reply <- serBoundsResult{ok: ok, first: first, last: last, err: err}
		case serPrune:
			if err := s.inner.Prune(m.through); err != nil {
				s.fail(fmt.Errorf("failed to prune below index %d: %w", m.through, err))
				return
			}
		case serIterator:
			it, err := s.inner.Iterator(m.startIndex)
			m.reply <- serIteratorResult{it: it, err: err}
		case serClose:
			m.done <- s.inner.Close()
			// FIFO guarantees every prior append has been delegated. Forbid further pushes so any
			// racing/future schedule aborts instead of deadlocking against the now-exiting serializer.
			s.senderCancel()
			return
		}
	}
}

// fail records the first fatal background error and triggers shutdown of the pipeline.
func (s *serializingWAL[T]) fail(err error) {
	s.asyncErr.CompareAndSwap(nil, &err)
	s.cancel()
	logger.Error("serializing WAL encountered a fatal error", "err", err)
}

// asyncError returns the first fatal background error, or nil if none occurred.
func (s *serializingWAL[T]) asyncError() error {
	if p := s.asyncErr.Load(); p != nil {
		return *p
	}
	return nil
}

var _ Iterator[[]byte] = (*serializingIterator[[]byte])(nil)

// serializingIterator adapts an inner byte iterator to a typed iterator by running deserialize inline in Next.
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
