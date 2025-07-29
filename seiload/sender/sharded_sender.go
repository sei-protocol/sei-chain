package sender

import (
	"fmt"
	"sync"

	"seiload/config"
	"seiload/stats"
	"seiload/types"
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
func (s *ShardedSender) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, worker := range s.workers {
		worker.Start()
	}
}

// Stop gracefully shuts down all workers
func (s *ShardedSender) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, worker := range s.workers {
		worker.Stop()
	}
}

// Send implements TxSender interface - calculates shard ID and routes to appropriate worker
func (s *ShardedSender) Send(tx *types.LoadTx) error {
	if tx == nil {
		return fmt.Errorf("transaction is nil")
	}

	// Calculate shard ID based on the transaction
	shardID := tx.ShardID(s.numShards)

	// Validate shard ID
	if shardID < 0 || shardID >= s.numShards {
		return fmt.Errorf("invalid shard ID %d for %d shards", shardID, s.numShards)
	}

	// Send to the appropriate worker
	s.mu.RLock()
	worker := s.workers[shardID]
	s.mu.RUnlock()

	return worker.Send(tx)
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
