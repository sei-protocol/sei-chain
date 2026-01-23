//go:build slow_bench

// Package bench provides benchmarks for CommitStore.
package bench

import (
	"crypto/rand"
	"flag"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

var (
	flagKeysPerBlock = flag.Int("keys", 1000, "number of keys per block")
	flagNumBlocks    = flag.Int("blocks", 100, "number of blocks to commit")
)

const (
	// EVMStoreName simulates the EVM module store
	EVMStoreName = "evm"

	// EVM storage key: 20-byte address + 32-byte slot = 52 bytes
	KeySize   = 52
	ValueSize = 32
)

func newCommitStore(b *testing.B) *sc.CommitStore {
	b.Helper()
	dir := b.TempDir()

	cfg := config.StateCommitConfig{
		AsyncCommitBuffer: 10,
		SnapshotInterval:  100,
	}

	cs := sc.NewCommitStore(dir, logger.NewNopLogger(), cfg)
	cs.Initialize([]string{EVMStoreName})

	if _, err := cs.LoadVersion(0, false); err != nil {
		b.Fatalf("failed to init CommitStore: %v", err)
	}

	b.Cleanup(func() {
		_ = cs.Close()
	})

	return cs
}

func generateChangesets(numBlocks, keysPerBlock int) []*proto.NamedChangeSet {
	cs := make([]*proto.NamedChangeSet, numBlocks)
	for i := range cs {
		pairs := make([]*iavl.KVPair, keysPerBlock)
		for j := range pairs {
			key := make([]byte, KeySize)
			val := make([]byte, ValueSize)
			_, _ = rand.Read(key)
			_, _ = rand.Read(val)
			pairs[j] = &iavl.KVPair{Key: key, Value: val}
		}
		cs[i] = &proto.NamedChangeSet{
			Name:      EVMStoreName,
			Changeset: iavl.ChangeSet{Pairs: pairs},
		}
	}
	return cs
}

// ProgressReporter reports benchmark progress periodically.
type ProgressReporter struct {
	totalKeys   int
	totalBlocks int
	keysWritten atomic.Int64
	startTime   time.Time
	done        chan struct{}
	interval    time.Duration
}

// NewProgressReporter creates a new progress reporter.
func NewProgressReporter(totalKeys, totalBlocks int, interval time.Duration) *ProgressReporter {
	return &ProgressReporter{
		totalKeys:   totalKeys,
		totalBlocks: totalBlocks,
		done:        make(chan struct{}),
		interval:    interval,
	}
}

// Start begins periodic progress reporting in a background goroutine.
func (p *ProgressReporter) Start() {
	p.startTime = time.Now()
	go func() {
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			select {
			case <-p.done:
				return
			case <-ticker.C:
				p.report()
			}
		}
	}()
}

// Add records that keys were written.
func (p *ProgressReporter) Add(keys int) {
	p.keysWritten.Add(int64(keys))
}

// Stop stops the progress reporter and prints final stats.
func (p *ProgressReporter) Stop() {
	close(p.done)
	elapsed := time.Since(p.startTime).Seconds()
	keys := p.keysWritten.Load()
	fmt.Printf("[Final] keys=%d/%d, keys/sec=%.0f, elapsed=%.2fs\n",
		keys, p.totalKeys, float64(keys)/elapsed, elapsed)
}

func (p *ProgressReporter) report() {
	keys := p.keysWritten.Load()
	elapsed := time.Since(p.startTime).Seconds()
	if elapsed > 0 {
		blocks := keys / int64(p.totalKeys/p.totalBlocks)
		fmt.Printf("[Progress] blocks=%d/%d, keys=%d/%d, keys/sec=%.0f\n",
			blocks, p.totalBlocks, keys, p.totalKeys, float64(keys)/elapsed)
	}
}

func runBenchmark(b *testing.B, keysPerBlock, numBlocks int) {
	changesets := generateChangesets(numBlocks, keysPerBlock)
	totalKeys := keysPerBlock * numBlocks

	b.ResetTimer()
	b.ReportAllocs()

	for i := range b.N {
		b.StopTimer()
		cs := newCommitStore(b)
		b.StartTimer()

		for block := 0; block < numBlocks; block++ {
			if err := cs.ApplyChangeSets([]*proto.NamedChangeSet{changesets[block]}); err != nil {
				b.Fatalf("block %d: apply failed: %v", block, err)
			}
			_ = cs.WorkingCommitInfo() // get commit hash
			if _, err := cs.Commit(); err != nil {
				b.Fatalf("block %d: commit failed: %v", block, err)
			}
		}

		b.StopTimer()
		elapsed := b.Elapsed().Seconds()
		b.ReportMetric(float64(totalKeys)/elapsed, "keys/sec")
	}
}

// runBenchmarkWithProgress runs the benchmark with periodic progress reporting.
// Reports keys/sec every 5 seconds to stdout.
func runBenchmarkWithProgress(b *testing.B, keysPerBlock, numBlocks int) {
	changesets := generateChangesets(numBlocks, keysPerBlock)
	totalKeys := keysPerBlock * numBlocks

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		cs := newCommitStore(b)

		// Start progress reporter
		progress := NewProgressReporter(totalKeys, numBlocks, 5*time.Second)
		progress.Start()

		b.StartTimer()

		for block := 0; block < numBlocks; block++ {
			if err := cs.ApplyChangeSets([]*proto.NamedChangeSet{changesets[block]}); err != nil {
				progress.Stop()
				b.Fatalf("block %d: apply failed: %v", block, err)
			}
			_ = cs.WorkingCommitInfo()
			if _, err := cs.Commit(); err != nil {
				progress.Stop()
				b.Fatalf("block %d: commit failed: %v", block, err)
			}
			progress.Add(keysPerBlock)
		}

		b.StopTimer()
		progress.Stop()

		elapsed := b.Elapsed().Seconds()
		b.ReportMetric(float64(totalKeys)/elapsed, "keys/sec")
	}
}

// BenchmarkWriteThroughput measures write throughput with configurable parameters.
func BenchmarkWriteThroughput(b *testing.B) {
	b.Logf("keysPerBlock=%d, numBlocks=%d, totalKeys=%d",
		*flagKeysPerBlock, *flagNumBlocks, *flagKeysPerBlock**flagNumBlocks)

	runBenchmarkWithProgress(b, *flagKeysPerBlock, *flagNumBlocks)
}

// BenchmarkWriteWithDifferentBlockSize tests throughput with fixed 1M total keys,
// varying keysPerBlock and numBlocks to find optimal block size.
func BenchmarkWriteWithDifferentBlockSize(b *testing.B) {
	const totalKeys = 1_000_000

	// Different keys per block to test (numBlocks = totalKeys / keysPerBlock)
	keysPerBlockOptions := []int{1, 2, 10, 20, 100, 200, 1000, 2000}

	for _, keysPerBlock := range keysPerBlockOptions {
		numBlocks := totalKeys / keysPerBlock
		name := fmt.Sprintf("%d_keys_x_%d_blocks", keysPerBlock, numBlocks)
		b.Run(name, func(b *testing.B) {
			runBenchmark(b, keysPerBlock, numBlocks)
		})
	}
}
