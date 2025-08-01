package stats

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// BlockCollector subscribes to new blocks and tracks block metrics
type BlockCollector struct {
	mu sync.RWMutex

	// Cumulative data (for final stats)
	allBlockTimes []time.Duration // All block times
	allGasUsed    []uint64        // All gas used values
	maxBlockNum   uint64          // Highest block number seen
	lastBlockTime time.Time       // Timestamp of last block

	// Window-based data (for periodic reporting)
	windowBlockTimes []time.Duration // Block times in current window
	windowGasUsed    []uint64        // Gas used in current window
	windowStart      time.Time       // Start of current window

	// WebSocket connection
	client       *ethclient.Client
	subscription ethereum.Subscription
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup

	// Configuration
	wsEndpoint string
	running    bool
}

// NewBlockCollector creates a new block data collector
func NewBlockCollector(firstEndpoint string) *BlockCollector {
	// Convert HTTP endpoint to WebSocket endpoint (8545 -> 8546)
	wsEndpoint := strings.Replace(firstEndpoint, ":8545", ":8546", 1)
	wsEndpoint = strings.Replace(wsEndpoint, "http://", "ws://", 1)

	ctx, cancel := context.WithCancel(context.Background())

	return &BlockCollector{
		allBlockTimes:    make([]time.Duration, 0),
		allGasUsed:       make([]uint64, 0),
		windowBlockTimes: make([]time.Duration, 0),
		windowGasUsed:    make([]uint64, 0),
		wsEndpoint:       wsEndpoint,
		ctx:              ctx,
		cancel:           cancel,
	}
}

// Start begins block subscription and data collection
func (bc *BlockCollector) Run(ctx context.Context) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if bc.running {
		return fmt.Errorf("block collector already running")
	}

	// Connect to WebSocket endpoint
	client, err := ethclient.Dial(bc.wsEndpoint)
	if err != nil {
		return fmt.Errorf("failed to connect to WebSocket endpoint %s: %v", bc.wsEndpoint, err)
	}

	bc.client = client
	bc.running = true

	// Start the subscription goroutine
	bc.wg.Add(1)
	go bc.subscribeToBlocks()
	return nil
}

// Stop gracefully shuts down the block collector
func (bc *BlockCollector) Stop() {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if !bc.running {
		return
	}

	bc.running = false
	bc.cancel()

	if bc.subscription != nil {
		bc.subscription.Unsubscribe()
	}

	if bc.client != nil {
		bc.client.Close()
	}

	bc.wg.Wait()
	log.Printf("✅ Stopped block collector")
}

// subscribeToBlocks handles the WebSocket subscription to new blocks
func (bc *BlockCollector) subscribeToBlocks() {
	defer bc.wg.Done()

	headers := make(chan *types.Header)
	sub, err := bc.client.SubscribeNewHead(bc.ctx, headers)
	if err != nil {
		log.Printf("❌ Failed to subscribe to new blocks: %v", err)
		return
	}

	bc.mu.Lock()
	bc.subscription = sub
	bc.mu.Unlock()

	log.Printf("📡 Subscribed to new blocks on %s", bc.wsEndpoint)

	for {
		select {
		case err := <-sub.Err():
			log.Printf("❌ Block subscription error: %v", err)
			return

		case header := <-headers:
			bc.processNewBlock(header)

		case <-bc.ctx.Done():
			return
		}
	}
}

// processNewBlock processes a new block header and updates metrics
func (bc *BlockCollector) processNewBlock(header *types.Header) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	now := time.Now()
	blockNum := header.Number.Uint64()
	gasUsed := header.GasUsed

	// Update max block number
	if blockNum > bc.maxBlockNum {
		bc.maxBlockNum = blockNum
	}

	// Track gas used
	bc.allGasUsed = append(bc.allGasUsed, gasUsed)
	bc.windowGasUsed = append(bc.windowGasUsed, gasUsed)

	// Calculate time between blocks
	if !bc.lastBlockTime.IsZero() {
		timeBetween := now.Sub(bc.lastBlockTime)
		bc.allBlockTimes = append(bc.allBlockTimes, timeBetween)
		bc.windowBlockTimes = append(bc.windowBlockTimes, timeBetween)
	}

	bc.lastBlockTime = now

	// Limit history to prevent memory growth (keep last 1000 entries)
	if len(bc.allBlockTimes) > 1000 {
		bc.allBlockTimes = bc.allBlockTimes[len(bc.allBlockTimes)-1000:]
	}
	if len(bc.allGasUsed) > 1000 {
		bc.allGasUsed = bc.allGasUsed[len(bc.allGasUsed)-1000:]
	}
	if len(bc.windowBlockTimes) > 1000 {
		bc.windowBlockTimes = bc.windowBlockTimes[len(bc.windowBlockTimes)-1000:]
	}
	if len(bc.windowGasUsed) > 1000 {
		bc.windowGasUsed = bc.windowGasUsed[len(bc.windowGasUsed)-1000:]
	}
}

// GetBlockStats returns current block statistics
func (bc *BlockCollector) GetBlockStats() BlockStats {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	stats := BlockStats{
		MaxBlockNumber: bc.maxBlockNum,
		SampleCount:    len(bc.allBlockTimes),
	}

	// Calculate block time percentiles
	if len(bc.allBlockTimes) > 0 {
		sortedTimes := make([]time.Duration, len(bc.allBlockTimes))
		copy(sortedTimes, bc.allBlockTimes)
		sort.Slice(sortedTimes, func(i, j int) bool {
			return sortedTimes[i] < sortedTimes[j]
		})

		stats.P50BlockTime = calculatePercentile(sortedTimes, 50)
		stats.P99BlockTime = calculatePercentile(sortedTimes, 99)
		stats.MaxBlockTime = sortedTimes[len(sortedTimes)-1]
	}

	// Calculate gas used percentiles
	if len(bc.allGasUsed) > 0 {
		sortedGas := make([]uint64, len(bc.allGasUsed))
		copy(sortedGas, bc.allGasUsed)
		sort.Slice(sortedGas, func(i, j int) bool {
			return sortedGas[i] < sortedGas[j]
		})

		stats.P50GasUsed = calculateGasPercentile(sortedGas, 50)
		stats.P99GasUsed = calculateGasPercentile(sortedGas, 99)
		stats.MaxGasUsed = sortedGas[len(sortedGas)-1]
	}

	return stats
}

// GetWindowBlockStats returns current window-based block statistics
func (bc *BlockCollector) GetWindowBlockStats() BlockStats {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	stats := BlockStats{
		MaxBlockNumber: bc.maxBlockNum,
		SampleCount:    len(bc.windowBlockTimes),
	}

	// Calculate block time percentiles for current window
	if len(bc.windowBlockTimes) > 0 {
		sortedTimes := make([]time.Duration, len(bc.windowBlockTimes))
		copy(sortedTimes, bc.windowBlockTimes)
		sort.Slice(sortedTimes, func(i, j int) bool {
			return sortedTimes[i] < sortedTimes[j]
		})

		stats.P50BlockTime = calculatePercentile(sortedTimes, 50)
		stats.P99BlockTime = calculatePercentile(sortedTimes, 99)
		stats.MaxBlockTime = sortedTimes[len(sortedTimes)-1]
	}

	// Calculate gas used percentiles for current window
	if len(bc.windowGasUsed) > 0 {
		sortedGas := make([]uint64, len(bc.windowGasUsed))
		copy(sortedGas, bc.windowGasUsed)
		sort.Slice(sortedGas, func(i, j int) bool {
			return sortedGas[i] < sortedGas[j]
		})

		stats.P50GasUsed = calculateGasPercentile(sortedGas, 50)
		stats.P99GasUsed = calculateGasPercentile(sortedGas, 99)
		stats.MaxGasUsed = sortedGas[len(sortedGas)-1]
	}

	return stats
}

// ResetWindowStats resets the window-based statistics for the next reporting period
func (bc *BlockCollector) ResetWindowStats() {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	bc.windowBlockTimes = make([]time.Duration, 0)
	bc.windowGasUsed = make([]uint64, 0)
	bc.windowStart = time.Now()
}

// calculateGasPercentile calculates the given percentile from sorted gas values
func calculateGasPercentile(sorted []uint64, percentile int) uint64 {
	if len(sorted) == 0 {
		return 0
	}

	index := (percentile * (len(sorted) - 1)) / 100
	if index >= len(sorted) {
		index = len(sorted) - 1
	}

	return sorted[index]
}

// BlockStats represents block-related statistics
type BlockStats struct {
	MaxBlockNumber uint64        `json:"max_block_number"`
	P50BlockTime   time.Duration `json:"p50_block_time"`
	P99BlockTime   time.Duration `json:"p99_block_time"`
	MaxBlockTime   time.Duration `json:"max_block_time"`
	P50GasUsed     uint64        `json:"p50_gas_used"`
	P99GasUsed     uint64        `json:"p99_gas_used"`
	MaxGasUsed     uint64        `json:"max_gas_used"`
	SampleCount    int           `json:"sample_count"`
}

// FormatBlockStats returns a formatted string representation of block statistics
func (bs *BlockStats) FormatBlockStats() string {
	if bs.SampleCount == 0 {
		return "block stats: no data available"
	}

	return fmt.Sprintf("block height=%d, times(p50=%v p99=%v max=%v), gas(p50=%d p99=%d max=%d) samples=%d",
		bs.MaxBlockNumber,
		bs.P50BlockTime.Round(time.Millisecond),
		bs.P99BlockTime.Round(time.Millisecond),
		bs.MaxBlockTime.Round(time.Millisecond),
		bs.P50GasUsed,
		bs.P99GasUsed,
		bs.MaxGasUsed,
		bs.SampleCount,
	)
}
