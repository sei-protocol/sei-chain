package wrappers

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	scTypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload"
)

var _ DBWrapper = (*historicalOffloadWrapper)(nil)
var _ offload.Stream = (*bufferedOffloadStream)(nil)
var _ io.Closer = (*bufferedOffloadStream)(nil)

type HistoricalOffloadStreamFactory func(
	ctx context.Context,
	dbDir string,
	ssConfig config.StateStoreConfig,
) (offload.Stream, error)

var (
	historicalOffloadStreamFactoryMu sync.RWMutex
	historicalOffloadStreamFactory   HistoricalOffloadStreamFactory = bufferedHistoricalOffloadStreamFactory
)

type historicalOffloadWrapper struct {
	stream  offload.Stream
	version atomic.Int64
}

// SetHistoricalOffloadStreamFactory overrides the transport used by the benchmark offload backend.
func SetHistoricalOffloadStreamFactory(factory HistoricalOffloadStreamFactory) {
	historicalOffloadStreamFactoryMu.Lock()
	defer historicalOffloadStreamFactoryMu.Unlock()

	if factory == nil {
		historicalOffloadStreamFactory = bufferedHistoricalOffloadStreamFactory
		return
	}
	historicalOffloadStreamFactory = factory
}

func currentHistoricalOffloadStreamFactory() HistoricalOffloadStreamFactory {
	historicalOffloadStreamFactoryMu.RLock()
	defer historicalOffloadStreamFactoryMu.RUnlock()
	return historicalOffloadStreamFactory
}

func newSSHistoricalOffloadStateStore(ctx context.Context, dbDir string, ssConfig *config.StateStoreConfig) (DBWrapper, error) {
	fmt.Printf("Opening historical offload stream from directory %s\n", dbDir)

	cfg := DefaultBenchStateStoreConfig()
	if ssConfig != nil {
		cfg = ssConfig
	}
	if cfg.Backend == "" {
		cfg.Backend = config.PebbleDBBackend
	}

	stream, err := currentHistoricalOffloadStreamFactory()(ctx, dbDir, *cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create historical offload stream: %w", err)
	}
	return NewHistoricalOffloadWrapper(stream), nil
}

func NewHistoricalOffloadWrapper(stream offload.Stream) DBWrapper {
	return &historicalOffloadWrapper{stream: stream}
}

func (h *historicalOffloadWrapper) ApplyChangeSets(entry *proto.ChangelogEntry) error {
	ack, err := h.stream.Publish(context.Background(), entry)
	if err != nil {
		return err
	}
	if !ack.Accepted {
		return fmt.Errorf("historical offload publish was not acknowledged at version %d", entry.Version)
	}
	h.version.Store(entry.Version)
	return nil
}

func (h *historicalOffloadWrapper) Read(_ []byte) (data []byte, found bool, err error) {
	return nil, false, nil
}

func (h *historicalOffloadWrapper) Commit() (int64, error) {
	return h.version.Load(), nil
}

func (h *historicalOffloadWrapper) Close() error {
	var streamErr error
	if closer, ok := h.stream.(io.Closer); ok {
		streamErr = closer.Close()
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

func bufferedHistoricalOffloadStreamFactory(
	_ context.Context,
	_ string,
	ssConfig config.StateStoreConfig,
) (offload.Stream, error) {
	return NewBufferedHistoricalOffloadStream(ssConfig.AsyncWriteBuffer), nil
}

// NewBufferedHistoricalOffloadStream provides a zero-dependency transport with queue backpressure.
func NewBufferedHistoricalOffloadStream(queueSize int) offload.Stream {
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

func (b *bufferedOffloadStream) Close() error {
	b.closeOnce.Do(func() {
		close(b.queue)
	})
	b.wg.Wait()
	return nil
}
