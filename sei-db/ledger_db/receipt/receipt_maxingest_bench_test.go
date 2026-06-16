package receipt

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
)

// BenchmarkReceiptMaxIngest measures a backend's true single-writer ingest
// ceiling. cryptosim caps at the rate its block producer (tx generation +
// FlatKV state commits) can feed the receipt store, so its receipts/s is a
// floor, not the store's limit. Here several producer goroutines pre-build
// receipt batches (unique tx hashes per block) into a channel and ONE writer
// goroutine calls SetReceipts as fast as it can drain the channel — the same
// single-writer model cryptosim and production use, with the producer made
// fast enough that the store is the bottleneck.
//
// Run with a single iteration (re-running would re-use the store and repeat
// tx hashes):
//
//	go test ./sei-db/ledger_db/receipt/ -run '^$' -bench BenchmarkReceiptMaxIngest -benchtime=1x -timeout 30m
func BenchmarkReceiptMaxIngest(b *testing.B) {
	for _, backend := range []string{"littidx", "littdb", "pebbleidx", "pebblev3"} {
		b.Run(backend, func(b *testing.B) { benchMaxIngest(b, backend) })
	}
}

func benchMaxIngest(b *testing.B, backend string) {
	const (
		receiptsPerBlock = 1024
		totalBlocks      = 30000 // ~30.7M receipts; below KeepRecent so no pruning mid-run
		producers        = 8
		channelDepth     = 256
	)

	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory = b.TempDir()
	cfg.KeepRecent = 100000
	cfg.PruneIntervalSeconds = 60
	cfg.Backend = backend

	store, err := newReceiptBackend(cfg, storeKey)
	if err != nil {
		b.Fatalf("failed to create %s store: %v", backend, err)
	}
	b.Cleanup(func() { _ = store.Close() })

	// Pools sized for realistic cardinality (cryptosim-like): one ERC20-style
	// log per receipt, an event-signature topic and an indexed-address topic.
	addrs := addressPool(200)
	topic0s := topicPool(16, 0x01)
	topic1s := topicPool(4000, 0x02)

	type job struct {
		block uint64
		batch []ReceiptRecord
	}
	jobs := make(chan job, channelDepth)
	var nextBlock int64

	var producerWg sync.WaitGroup
	for p := 0; p < producers; p++ {
		producerWg.Add(1)
		go func() {
			defer producerWg.Done()
			for {
				blk := atomic.AddInt64(&nextBlock, 1)
				if blk > totalBlocks {
					return
				}
				// seed = blk*receiptsPerBlock gives every receipt across the
				// whole run a unique tx hash (hashFromUint64 is injective), so
				// litt secondary keys never collide.
				seed := uint64(blk) * receiptsPerBlock
				batch := makeDiverseReceiptBatch(uint64(blk), receiptsPerBlock, seed, addrs, topic0s, topic1s, nil)
				jobs <- job{block: uint64(blk), batch: batch}
			}
		}()
	}
	go func() { producerWg.Wait(); close(jobs) }()

	b.ResetTimer()
	start := time.Now()
	written := 0
	for j := range jobs {
		blockCtx := ctx.WithBlockHeight(int64(j.block)) //nolint:gosec // block fits int64
		if err := store.SetReceipts(blockCtx, j.batch); err != nil {
			b.Fatalf("SetReceipts failed at block %d: %v", j.block, err)
		}
		written += len(j.batch)
	}
	elapsed := time.Since(start)
	b.StopTimer()

	b.ReportMetric(float64(written)/elapsed.Seconds(), "receipts/s")
	b.ReportMetric(elapsed.Seconds(), "write_s")
	b.ReportMetric(float64(written), "receipts_total")
}
