package seiwal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// serAppend carries a framed-payload producer to the serializer goroutine. The closure captures the typed
// item so this message type stays non-generic — T never enters the channel's dynamic type, which keeps the
// serializer loop's type switch free of type parameters.
type serAppend struct {
	index     uint64
	data []byte	
}

// serFlush asks the serializer goroutine to flush the inner WAL, signaling done when durable.
type serFlush struct {
	done chan struct{}
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
}

// serPrune asks the serializer goroutine to prune the inner WAL below `through`.
type serPrune struct {
	through uint64
}

// serIterator asks the serializer goroutine to create an inner iterator, ordered after every prior append.
type serIterator struct {
	startIndex uint64
	endIndex   uint64
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
	config *Config
	
	// Serializes a payload to bytes; runs on the serializer goroutine.
	serialize func(T) []byte
	// Deserializes stored bytes back to a payload; runs inline in the iterator.
	deserialize func([]byte) (T, error)

	// Records this instance's measurements, or drops them when its metrics are disabled. Read-only after
	// construction.
	metrics walMetrics

	// Caller entry points funnel through serializerChan as a single ordered stream to the serializer.
	serializerChan chan any
}

func newSerializingWAL[T any](
	config *Config,
	serialize func(T) []byte,
	deserialize func([]byte) (T, error),
) *serializingWAL[T] {
	return &serializingWAL[T]{
		serialize:      serialize,
		deserialize:    deserialize,
		metrics:        newWALMetrics(config, "serializer"),
		serializerChan: make(chan any, config.SerializerBufferSize),
	}
}

func (s *serializingWAL[T]) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, sc scope.Scope) error {
		sc.Spawn(func() error { return s.sampleQueueDepth(ctx) })
		return s.serializerLoop(ctx)
	})
}

// sampleQueueDepth periodically records the serializer channel's buffered depth until Close stops it
// (samplerStop) or a fatal shutdown cancels ctx.
func (s *serializingWAL[T]) sampleQueueDepth(ctx context.Context) error {
	if s.config.DisableMetrics || s.config.MetricsSampleInterval <= 0 {
		return nil
	}
	ticker := time.NewTicker(s.config.MetricsSampleInterval)
	defer ticker.Stop()
	for {
		if _,err := utils.Recv(ctx,ticker.C); err!=nil {
			return err
		}
		s.metrics.recordQueueDepth(ctx, len(s.serializerChan))
	}
}

// Append schedules a payload to be serialized and appended at the given index.
func (s *serializingWAL[T]) Append(ctx context.Context, index uint64, data T) error {
	return utils.Send(ctx, s.serializerChan, any(serAppend{index,s.serialize(data)}))
}

// Flush blocks until all previously scheduled appends are durable.
func (s *serializingWAL[T]) Flush(ctx context.Context) error {
	done := make(chan struct{})
	if err := utils.Send(ctx,s.serializerChan,any(serFlush{done: done})); err != nil {
		return err 
	}
	_,_,err := utils.RecvOrClosed(ctx,done)
	return err
}

// Bounds reports the range of record indices stored in the WAL.
func (s *serializingWAL[T]) Bounds(ctx context.Context) (bool, uint64, uint64, error) {
	reply := make(chan serBoundsResult, 1)
	if err := utils.Send(ctx,s.serializerChan,any(serBounds{reply: reply})); err != nil {
		return false, 0, 0, err 
	}
	r,err := utils.Recv(ctx,reply)
	if err!=nil { return false, 0, 0, err }
	return r.ok, r.first, r.last, nil
}

// PruneBefore schedules removal of whole inner files below lowestIndexToKeep. It does not block on completion.
func (s *serializingWAL[T]) PruneBefore(ctx context.Context, lowestIndexToKeep uint64) error {
	return utils.Send(ctx, s.serializerChan, any(serPrune{through: lowestIndexToKeep}))
}

// Iterator returns an iterator over the inclusive index range [startIndex, endIndex]. Construction is ordered
// on the serializer goroutine after every prior append, so the iterator observes all previously scheduled
// appends.
func (s *serializingWAL[T]) Iterator(ctx context.Context, startIndex uint64, endIndex uint64) (Iterator[T], error) {
	reply := make(chan serIteratorResult, 1)
	if err := utils.Send(ctx, s.serializerChan, any(serIterator{startIndex: startIndex, endIndex: endIndex, reply: reply})); err != nil {
		return nil, err 
	}
	r,err := utils.Recv(ctx,reply)
	if err != nil { return nil, err }
	return &serializingIterator[T]{inner: r.it, deserialize: s.deserialize}, nil
}

// serializerLoop serializes each append's payload and delegates it to the inner WAL, handling control
// messages (flush, bounds, prune, iterator, close) in FIFO order relative to appends so they observe a
// consistent view. Runs on its own goroutine until close or a fatal error.
func (s *serializingWAL[T]) serializerLoop(ctx context.Context) error {
	wal, err := newWAL(s.config)
	if err != nil {
		return fmt.Errorf("failed to open inner WAL: %w", err)
	}
	defer wal.Close()
	for {
		msg,err := utils.Recv(ctx,s.serializerChan)
		if err!=nil { return err }
		switch m := msg.(type) {
		case serAppend:
			start := time.Now()
			if err != nil {
				return fmt.Errorf("failed to serialize record for index %d: %w", m.index, err)
			}
			s.metrics.recordSerialized(ctx, time.Since(start), len(m.data))
			if err := wal.Append(m.index, m.data); err != nil {
				return fmt.Errorf("failed to append record for index %d: %w", m.index, err)
			}
		case serFlush:
			if err := wal.Flush(); err != nil {
				return fmt.Errorf("failed to flush: %w", err)
			}
			close(m.done)
		case serBounds:
			ok, first, last, err := wal.Bounds()
			if err != nil {
				return fmt.Errorf("bounds query failed: %w", err)
			}
			// NON-BLOCKING, capacity is 1, and this is the only writer.
			m.reply <- serBoundsResult{ok: ok, first: first, last: last}
		case serPrune:
			if err := wal.PruneBefore(m.through); err != nil {
				return fmt.Errorf("failed to prune below index %d: %w", m.through, err)
			}
		case serIterator:
			it, err := wal.Iterator(m.startIndex, m.endIndex)
			// A rejected range leaves the inner WAL healthy, so mirror that here; only a genuine inner
			// failure bricks the serializing layer.
			if err != nil && !errors.Is(err, ErrIteratorRange) {
				return fmt.Errorf("failed to create iterator: %w", err)
			}
			// NON-BLOCKING, capacity is 1, and this is the only writer.
			m.reply <- serIteratorResult{it: it, err: err}
		}
	}
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
		it.entry = utils.Zero[T]() 
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
