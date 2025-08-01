package sender

import (
	"fmt"
	"sync"
	"context"

	"github.com/sei-protocol/sei-chain/seiload/config"
	"github.com/sei-protocol/sei-chain/seiload/stats"
	"github.com/sei-protocol/sei-chain/seiload/types"
	"github.com/sei-protocol/sei-chain/utils2/service"
)

// ShardedSender implements TxSender with multiple workers, one per endpoint
type ShardedSender struct {
	workers    []*Worker
	numShards  int
	bufferSize int
	dryRun     bool
	debug      bool
	mu         sync.RWMutex
	collector  *stats.Collector
	logger     *stats.Logger
}

// NewShardedSender creates a new sharded sender with workers for each endpoint
func NewShardedSender(cfg *config.LoadConfig, bufferSize int, workers int) (*ShardedSender, error) {
	if len(cfg.Endpoints) == 0 {
		return nil, fmt.Errorf("no endpoints configured")
	}

	workerList := make([]*Worker, len(cfg.Endpoints))
	for i, endpoint := range cfg.Endpoints {
		workerList[i] = NewWorker(i, endpoint, bufferSize, workers)
	}

	return &ShardedSender{
		workers:    workerList,
		numShards:  len(cfg.Endpoints),
		bufferSize: bufferSize,
	}, nil
}

// Start initializes and starts all workers
func (s *ShardedSender) Run(ctx context.Context) error {
	s.mu.Lock()
	workers := s.workers
	s.mu.Unlock()
	return service.Run(ctx, func(ctx context.Context, s service.Scope) error {
		for _, worker := range workers {
			s.Spawn(func() error { return worker.Run(ctx) })
		}
		return nil
	})
}

// Send implements TxSender interface - calculates shard ID and routes to appropriate worker
func (s *ShardedSender) Send(ctx context.Context, tx *types.LoadTx) error {
	// Calculate shard ID based on the transaction
	shardID := tx.ShardID(s.numShards)

	// Send to the appropriate worker
	s.mu.RLock()
	worker := s.workers[shardID]
	s.mu.RUnlock()

	return worker.Send(ctx, tx)
}

// GetWorkerStats returns statistics for all workers
func (s *ShardedSender) GetWorkerStats() []WorkerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := make([]WorkerStats, len(s.workers))
	for i, worker := range s.workers {
		stats[i] = WorkerStats{
			WorkerID:      i,
			Endpoint:      worker.GetEndpoint(),
			ChannelLength: worker.GetChannelLength(),
		}
	}
	return stats
}

// WorkerStats contains statistics for a single worker
type WorkerStats struct {
	WorkerID      int
	Endpoint      string
	ChannelLength int
}

// GetNumShards returns the number of shards (workers)
func (s *ShardedSender) GetNumShards() int {
	return s.numShards
}

// SetDryRun sets the dry-run flag for the sender and its workers
func (s *ShardedSender) SetDryRun(dryRun bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.dryRun = dryRun
	for _, worker := range s.workers {
		worker.SetDryRun(dryRun)
	}
}

func (s *ShardedSender) SetDebug(debug bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.debug = debug
	for _, worker := range s.workers {
		worker.SetDebug(debug)
	}
}

// SetTrackReceipts sets the track-receipts flag for the sender and its workers
func (s *ShardedSender) SetTrackReceipts(trackReceipts bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, worker := range s.workers {
		worker.SetTrackReceipts(trackReceipts)
	}
}

// SetTrackBlocks sets the track-blocks flag (placeholder - blocks are tracked separately)
func (s *ShardedSender) SetTrackBlocks(trackBlocks bool) {
	// Block tracking is handled by the BlockCollector, not the sender
	// This method exists for consistency with the CLI interface
}

// SetStatsCollector sets the statistics collector for all workers
func (s *ShardedSender) SetStatsCollector(collector *stats.Collector, logger *stats.Logger) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.collector = collector
	s.logger = logger

	// Pass to all workers
	for _, worker := range s.workers {
		worker.SetStatsCollector(collector, logger)
	}
}
