package wrappers

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	dbTypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	scTypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	ssComposite "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/composite"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload"
)

var _ DBWrapper = (*historicalOffloadWrapper)(nil)
var _ offload.Stream = (*bufferedOffloadStream)(nil)
var _ io.Closer = (*bufferedOffloadStream)(nil)

// historicalOffloadWrapper exercises the production SS historical offload write
// path. Reads are intentionally omitted because the benchmark only cares about
// how quickly changelog entries can be accepted and drained.
type historicalOffloadWrapper struct {
	base    dbTypes.StateStore
	stream  offload.Stream
	version atomic.Int64
}

func newSSHistoricalOffloadStateStore(dbDir string) (DBWrapper, error) {
	fmt.Printf("Opening composite state store with historical offload from directory %s\n", dbDir)

	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = config.PebbleDBBackend
	cfg.AsyncWriteBuffer = config.DefaultSSAsyncBuffer
	cfg.WriteMode = config.SplitWrite
	cfg.ReadMode = config.EVMFirstRead

	stream := newBufferedOffloadStream(cfg.AsyncWriteBuffer)
	store, err := ssComposite.NewCompositeStateStoreWithOffload(cfg, dbDir, stream)
	if err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("failed to open composite state store with historical offload: %w", err)
	}

	return NewHistoricalOffloadWrapper(store, stream), nil
}

func NewHistoricalOffloadWrapper(store dbTypes.StateStore, stream offload.Stream) DBWrapper {
	w := &historicalOffloadWrapper{
		base:   store,
		stream: stream,
	}
	w.version.Store(store.GetLatestVersion())
	return w
}

func (h *historicalOffloadWrapper) ApplyChangeSets(cs []*proto.NamedChangeSet) error {
	nextVersion := h.version.Add(1)
	return h.base.ApplyChangesetAsync(nextVersion, cs)
}

func (h *historicalOffloadWrapper) Read(_ []byte) (data []byte, found bool, err error) {
	return nil, false, nil
}

func (h *historicalOffloadWrapper) Commit() (int64, error) {
	return h.version.Load(), nil
}

func (h *historicalOffloadWrapper) Close() error {
	baseErr := h.base.Close()

	var streamErr error
	if closer, ok := h.stream.(io.Closer); ok {
		streamErr = closer.Close()
	}

	if baseErr != nil {
		return baseErr
	}
	return streamErr
}

func (h *historicalOffloadWrapper) Version() int64 {
	return h.version.Load()
}

func (h *historicalOffloadWrapper) LoadVersion(_ int64) error {
	return nil
}

func (h *historicalOffloadWrapper) Importer(_ int64) (scTypes.Importer, error) {
	return nil, fmt.Errorf("import not supported for historical offload wrapper")
}

func (h *historicalOffloadWrapper) GetPhaseTimer() *metrics.PhaseTimer {
	return nil
}

// bufferedOffloadStream is a tiny in-process stand-in for an async transport.
// Publish blocks when the queue is full, which gives the benchmark realistic
// backpressure without coupling it to a real remote system.
type bufferedOffloadStream struct {
	queue     chan *proto.ChangelogEntry
	closeOnce sync.Once
	wg        sync.WaitGroup
}

func newBufferedOffloadStream(queueSize int) *bufferedOffloadStream {
	if queueSize < 1 {
		queueSize = 1
	}

	stream := &bufferedOffloadStream{
		queue: make(chan *proto.ChangelogEntry, queueSize),
	}
	stream.wg.Add(1)
	go func() {
		defer stream.wg.Done()
		for range stream.queue {
		}
	}()

	return stream
}

func (b *bufferedOffloadStream) Publish(ctx context.Context, entry *proto.ChangelogEntry) (offload.Ack, error) {
	if entry == nil {
		return offload.Ack{Accepted: true}, nil
	}

	select {
	case b.queue <- entry:
		return offload.Ack{Accepted: true}, nil
	case <-ctx.Done():
		return offload.Ack{}, ctx.Err()
	}
}

func (b *bufferedOffloadStream) Replay(_ context.Context, _ offload.ReplayRequest, _ func(*proto.ChangelogEntry) error) error {
	return nil
}

func (b *bufferedOffloadStream) Close() error {
	b.closeOnce.Do(func() {
		close(b.queue)
	})
	b.wg.Wait()
	return nil
}
