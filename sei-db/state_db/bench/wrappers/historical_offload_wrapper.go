package wrappers

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	scTypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload"
)

var _ DBWrapper = (*historicalOffloadWrapper)(nil)

type HistoricalOffloadStreamFactory func(
	ctx context.Context,
	dbDir string,
	ssConfig config.StateStoreConfig,
) (offload.Stream, error)

var historicalOffloadStreamFactory HistoricalOffloadStreamFactory

type historicalOffloadWrapper struct {
	stream  offload.Stream
	version atomic.Int64
}

// SetHistoricalOffloadStreamFactory overrides the transport used by the benchmark offload backend.
func SetHistoricalOffloadStreamFactory(factory HistoricalOffloadStreamFactory) {
	historicalOffloadStreamFactory = factory
}

func currentHistoricalOffloadStreamFactory() HistoricalOffloadStreamFactory {
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

	factory := currentHistoricalOffloadStreamFactory()
	if factory == nil {
		return nil, fmt.Errorf("historical offload stream factory is not configured")
	}
	stream, err := factory(ctx, dbDir, *cfg)
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
