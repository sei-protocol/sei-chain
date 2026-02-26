package receipt

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/sei-protocol/sei-chain/evmrpc/ethbloom"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	readBenchWriteBlocks      = 2000
	readBenchReceiptsPerBlock = 3000
	readBenchNumAddresses     = 50
	readBenchNumTopic0        = 7  // coprime with numAddresses so all (addr, topic0) combos occur
	readBenchNumTopic1        = 97 // coprime with both

	// benchWorkerBatchSize matches evmrpcconfig.WorkerBatchSize used by
	// the production GetLogsByFilters fallback path in evmrpc/filter.go.
	benchWorkerBatchSize = 100
)

type rangeConfig struct {
	size  int
	store bool // true → target early blocks (outside cache); false → target tail (cache)
}

var (
	readBenchRanges = []rangeConfig{
		{100, false},
		{500, false},
		{100, true},
		{500, true},
	}
	readBenchConcurrences = []int{1, 2, 4, 8, 16}
)

type filterConfig struct {
	name string
	crit func(env *readBenchEnv) filters.FilterCriteria
}

var readBenchFilters = []filterConfig{
	{
		name: "none",
		crit: func(_ *readBenchEnv) filters.FilterCriteria {
			return filters.FilterCriteria{}
		},
	},
	{
		name: "address",
		crit: func(env *readBenchEnv) filters.FilterCriteria {
			return filters.FilterCriteria{
				Addresses: []common.Address{env.addrs[0]},
			}
		},
	},
	{
		name: "topic0",
		crit: func(env *readBenchEnv) filters.FilterCriteria {
			return filters.FilterCriteria{
				Topics: [][]common.Hash{{env.topic0s[0]}},
			}
		},
	},
	{
		name: "address+topic0",
		crit: func(env *readBenchEnv) filters.FilterCriteria {
			return filters.FilterCriteria{
				Addresses: []common.Address{env.addrs[0]},
				Topics:    [][]common.Hash{{env.topic0s[0]}},
			}
		},
	},
}

// BenchmarkReceiptReadStore100 is a focused benchmark: blocks=[1,100] (store-only),
// concurrency=16, filter=address+topic0, for both pebble and parquet.
func BenchmarkReceiptReadStore100(b *testing.B) {
	for _, backend := range []string{receiptBackendPebble, receiptBackendParquet} {
		b.Run(backend, func(b *testing.B) {
			// Setup in parent scope so the probe run (b.N=1) and real run share it.
			env := setupReadBenchmark(b, backend,
				readBenchWriteBlocks, readBenchReceiptsPerBlock,
				readBenchNumAddresses, readBenchNumTopic0, readBenchNumTopic1,
			)

			rc := rangeConfig{size: 100, store: true}
			crit := filters.FilterCriteria{
				Addresses: []common.Address{env.addrs[0]},
				Topics:    [][]common.Hash{{env.topic0s[0]}},
			}
			b.Run("query", func(b *testing.B) {
				runFilterLogsBenchmark(b, env, backend, rc, 16, crit)
			})
		})
	}
}

// This takes very long to run, use regex to run specific benchmarks, example:
// Example: go test -run='^$' -bench 'BenchmarkReceiptRead/.*/range=100_store/concurrency=4/filter=address.topic0' -benchtime=3x -count=1 -timeout=30m ./sei-db/ledger_db/receipt/
func BenchmarkReceiptRead(b *testing.B) {
	backends := []string{receiptBackendPebble, receiptBackendParquet}

	for _, backend := range backends {
		b.Run(backend, func(b *testing.B) {
			env := setupReadBenchmark(b, backend,
				readBenchWriteBlocks, readBenchReceiptsPerBlock,
				readBenchNumAddresses, readBenchNumTopic0, readBenchNumTopic1,
			)

			fmt.Printf("[bench %s] starting read benchmarks ...\n", backend)

			for _, rc := range readBenchRanges {
				label := fmt.Sprintf("range=%d", rc.size)
				if rc.store {
					label += "_store"
				} else {
					label += "_cache"
				}
				b.Run(label, func(b *testing.B) {
					for _, conc := range readBenchConcurrences {
						b.Run(fmt.Sprintf("concurrency=%d", conc), func(b *testing.B) {
							for _, fc := range readBenchFilters {
								crit := fc.crit(env)
								b.Run(fmt.Sprintf("filter=%s", fc.name), func(b *testing.B) {
									runFilterLogsBenchmark(b, env, backend, rc, conc, crit)
								})
							}
						})
					}
				})
			}
		})
	}
}

// runFilterLogsBenchmark executes b.N FilterLogs queries spread across
// the given number of concurrent goroutines.
//
// rc.store=false → target the most recent blocks (cache hits).
// rc.store=true  → target early blocks outside the cache window (store reads).
func runFilterLogsBenchmark(b *testing.B, env *readBenchEnv, backend string, rc rangeConfig, concurrency int, crit filters.FilterCriteria) {
	b.Helper()

	var fromBlock, toBlock uint64
	if rc.store {
		fromBlock = 1
		toBlock = uint64(rc.size)
	} else {
		toBlock = uint64(env.blocks)
		fromBlock = toBlock - uint64(rc.size) + 1
	}

	cacheHint := "cache"
	if rc.store {
		cacheHint = "store"
	}
	fmt.Printf("[bench %s] FilterLogs blocks=[%d,%d] concurrency=%d filter=%s (%s)\n",
		backend, fromBlock, toBlock, concurrency, filterName(crit), cacheHint)

	b.ResetTimer()

	var remaining atomic.Int64
	remaining.Store(int64(b.N))

	// Track the number of logs per query for reporting, -1 means not yet set
	// using atomic int64 to avoid race conditions
	var logsPerQuery atomic.Int64
	logsPerQuery.Store(-1)

	fmt.Printf("[bench %s] Starting benchmark queries\n", backend)
	var wg sync.WaitGroup
	for g := 0; g < concurrency; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for remaining.Add(-1) >= 0 {
				logs, qerr := execFilterLogs(env, fromBlock, toBlock, crit)
				if qerr != nil {
					b.Errorf("FilterLogs failed: %v", qerr)
					return
				}
				logsPerQuery.CompareAndSwap(-1, int64(len(logs)))
			}
		}()
	}
	wg.Wait()

	b.StopTimer()

	elapsed := b.Elapsed()
	lpq := logsPerQuery.Load()
	if elapsed > 0 && b.N > 0 {
		queriesPerSec := float64(b.N) / elapsed.Seconds()
		b.ReportMetric(queriesPerSec, "queries/s")

		if lpq >= 0 {
			b.ReportMetric(float64(lpq), "logs/query")
		}

		if lpq > 0 {
			totalLogs := int64(b.N) * lpq
			nsPerLog := float64(elapsed.Nanoseconds()) / float64(totalLogs)
			b.ReportMetric(nsPerLog, "ns/log")
		}
	}
}

// execFilterLogs dispatches to the correct read path for the backend.
func execFilterLogs(env *readBenchEnv, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	if env.isPebble {
		return pebbleFilterLogs(env.store, env.ctx, fromBlock, toBlock, env.idx, crit)
	}
	return env.store.FilterLogs(env.ctx, fromBlock, toBlock, crit)
}

// readBenchEnv holds everything produced by the write phase that the read
// benchmarks need.
type readBenchEnv struct {
	store    ReceiptStore
	ctx      sdk.Context
	idx      *readBenchIndex
	addrs    []common.Address
	topic0s  []common.Hash
	topic1s  []common.Hash
	blocks   int
	isPebble bool
}

// setupReadBenchmark writes diverse receipt data through the cached layer and
// returns an environment ready for read benchmarks.  The write phase is not
// timed.  backend must be "pebble" or "parquet".
func setupReadBenchmark(b *testing.B, backend string, blocks, receiptsPerBlock, numAddrs, numTopic0, numTopic1 int) *readBenchEnv {
	b.Helper()

	ctx, storeKey := newTestContext()

	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory = b.TempDir()
	cfg.KeepRecent = 0
	cfg.PruneIntervalSeconds = 0
	cfg.Backend = backend

	store, err := NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	if err != nil {
		b.Fatalf("failed to create %s receipt store: %v", backend, err)
	}
	b.Cleanup(func() { _ = store.Close() })

	cached, ok := store.(*cachedReceiptStore)
	if !ok {
		b.Fatalf("expected cachedReceiptStore, got %T", store)
	}

	// For parquet, set a reasonable flush interval so data lands in parquet files.
	if backend == receiptBackendParquet {
		pqs, ok := cached.backend.(*parquetReceiptStore)
		if !ok {
			b.Fatalf("expected parquetReceiptStore backend, got %T", cached.backend)
		}
		pqs.store.SetBlockFlushInterval(100)
	}

	addrs := addressPool(numAddrs)
	t0s := topicPool(numTopic0, 0x01)
	t1s := topicPool(numTopic1, 0x02)
	idx := newReadBenchIndex()

	fmt.Printf("[setup %s] writing %d blocks x %d receipts/block ...\n", backend, blocks, receiptsPerBlock)

	logInterval := blocks / 5 // log at ~20%, 40%, 60%, 80%, 100%
	if logInterval == 0 {
		logInterval = 1
	}

	var seed uint64
	for block := 0; block < blocks; block++ {
		blockNumber := uint64(block + 1)
		batch := makeDiverseReceiptBatch(blockNumber, receiptsPerBlock, seed, addrs, t0s, t1s, idx)
		if err := store.SetReceipts(ctx.WithBlockHeight(int64(blockNumber)), batch); err != nil {
			b.Fatalf("failed to write block %d: %v", blockNumber, err)
		}
		seed += uint64(receiptsPerBlock)

		if (block+1)%logInterval == 0 {
			fmt.Printf("[setup %s] wrote block %d/%d (%.0f%%)\n", backend, block+1, blocks, float64(block+1)/float64(blocks)*100)
		}
	}

	fmt.Printf("[setup %s] flushing async writes ...\n", backend)

	// Drain all async writes so the underlying store is fully populated.
	switch backend {
	case receiptBackendPebble:
		rs, ok := cached.backend.(*receiptStore)
		if !ok {
			b.Fatalf("expected receiptStore backend, got %T", cached.backend)
		}
		rs.db.WaitForPendingWrites()
	case receiptBackendParquet:
		pqs, ok := cached.backend.(*parquetReceiptStore)
		if !ok {
			b.Fatalf("expected parquetReceiptStore backend, got %T", cached.backend)
		}
		if err := pqs.store.Flush(); err != nil {
			b.Fatalf("failed to flush parquet: %v", err)
		}
	}

	fmt.Printf("[setup %s] done (%d blocks x %d receipts = %d total)\n", backend, blocks, receiptsPerBlock, blocks*receiptsPerBlock)

	return &readBenchEnv{
		store:    store,
		ctx:      ctx,
		idx:      idx,
		addrs:    addrs,
		topic0s:  t0s,
		topic1s:  t1s,
		blocks:   blocks,
		isPebble: backend == receiptBackendPebble,
	}
}

// pebbleFilterLogs mirrors the production evmrpc fallback path for pebble:
// iterate each block in [fromBlock, toBlock], use the block-level bloom to skip
// non-matching blocks, use per-tx bloom to skip non-matching receipts, then
// exact-match filter the remaining logs.
func pebbleFilterLogs(
	store ReceiptStore,
	ctx sdk.Context,
	fromBlock, toBlock uint64,
	txIndex *readBenchIndex,
	crit filters.FilterCriteria,
) ([]*ethtypes.Log, error) {
	hasFilters := len(crit.Addresses) > 0 || len(crit.Topics) > 0
	var filterIdxs [][]ethbloom.BloomIndexes
	if hasFilters {
		filterIdxs = ethbloom.EncodeFilters(crit.Addresses, crit.Topics)
	}

	var result []*ethtypes.Log
	for block := fromBlock; block <= toBlock; block++ {
		if hasFilters {
			blockBloom := txIndex.blockBlooms[block]
			if blockBloom != (ethtypes.Bloom{}) && !ethbloom.MatchFilters(blockBloom, filterIdxs) {
				continue
			}
		}

		hashes := txIndex.blockTxHashes[block]
		var logStartIndex uint
		for _, txHash := range hashes {
			receipt, err := store.GetReceipt(ctx, txHash)
			if err != nil {
				return nil, fmt.Errorf("block %d tx %s: %w", block, txHash.Hex(), err)
			}

			if hasFilters && len(receipt.LogsBloom) > 0 {
				txBloom := ethtypes.Bloom(receipt.LogsBloom)
				if !ethbloom.MatchFilters(txBloom, filterIdxs) {
					logStartIndex += uint(len(receipt.Logs))
					continue
				}
			}

			txLogs := getLogsForTx(receipt, logStartIndex)
			logStartIndex += uint(len(txLogs))
			for _, lg := range txLogs {
				if ethbloom.MatchesCriteria(lg, crit) {
					result = append(result, lg)
				}
			}
		}
	}
	return result, nil
}

// readBenchIndex tracks block-to-txHash mappings and block-level bloom filters
// built during the write phase.
type readBenchIndex struct {
	blockTxHashes map[uint64][]common.Hash
	blockBlooms   map[uint64]ethtypes.Bloom
}

func newReadBenchIndex() *readBenchIndex {
	return &readBenchIndex{
		blockTxHashes: make(map[uint64][]common.Hash),
		blockBlooms:   make(map[uint64]ethtypes.Bloom),
	}
}

// record indexes a single receipt's tx hash for its block.
func (idx *readBenchIndex) record(blockNumber uint64, txHash common.Hash) {
	idx.blockTxHashes[blockNumber] = append(idx.blockTxHashes[blockNumber], txHash)
}

// addressPool generates a deterministic set of unique contract addresses.
func addressPool(n int) []common.Address {
	pool := make([]common.Address, n)
	for i := range pool {
		var buf [20]byte
		binary.BigEndian.PutUint64(buf[12:], uint64(i)+1)
		buf[0] = 0xAA // prefix to distinguish from topic hashes
		pool[i] = common.BytesToAddress(buf[:])
	}
	return pool
}

// topicPool generates a deterministic set of unique topic hashes.
// The prefix byte differentiates pools (e.g. 0x01 for event sigs, 0x02 for indexed params).
func topicPool(n int, prefix byte) []common.Hash {
	pool := make([]common.Hash, n)
	for i := range pool {
		var buf [32]byte
		buf[0] = prefix
		binary.BigEndian.PutUint64(buf[24:], uint64(i)+1)
		pool[i] = common.BytesToHash(buf[:])
	}
	return pool
}

// makeDiverseReceiptBatch creates a batch of receipts with varied addresses and
// topics drawn from the provided pools.  Assignment is deterministic based on
// seed so results are reproducible.
//
// Each receipt gets exactly 1 log with:
//   - Address:  addrs[pick(seed+i, len(addrs))]
//   - Topic[0]: topic0s[pick(seed+i, len(topic0s))]   (event signature)
//   - Topic[1]: topic1s[pick(seed+i, len(topic1s))]   (indexed param)
//
// The function also records every log into idx for later correctness checking.
func makeDiverseReceiptBatch(
	blockNumber uint64,
	count int,
	seed uint64,
	addrs []common.Address,
	topic0s []common.Hash,
	topic1s []common.Hash,
	idx *readBenchIndex,
) []ReceiptRecord {
	records := make([]ReceiptRecord, count)

	logData := make([]byte, 32)
	var blockBloom ethtypes.Bloom

	for i := 0; i < count; i++ {
		txHash := hashFromUint64(seed + uint64(i))

		base := seed*3 + uint64(i)
		addr := addrs[pick(base, len(addrs))]
		t0 := topic0s[pick(base+1, len(topic0s))]
		t1 := topic1s[pick(base+2, len(topic1s))]

		receipt := &types.Receipt{
			TxHashHex:         txHash.Hex(),
			BlockNumber:       blockNumber,
			TransactionIndex:  uint32(i),
			GasUsed:           52000,
			CumulativeGasUsed: uint64(52000 * (i + 1)),
			Status:            1,
			Logs: []*types.Log{
				{
					Address: addr.Hex(),
					Topics:  []string{t0.Hex(), t1.Hex()},
					Data:    logData,
					Index:   0,
				},
			},
		}

		txBloom := ethtypes.CreateBloom(&ethtypes.Receipt{Logs: getLogsForTx(receipt, 0)})
		receipt.LogsBloom = txBloom.Bytes()
		for j := range blockBloom {
			blockBloom[j] |= txBloom[j]
		}

		receiptBytes, err := receipt.Marshal()
		if err != nil {
			panic(fmt.Sprintf("makeDiverseReceiptBatch: marshal failed: %v", err))
		}

		records[i] = ReceiptRecord{
			TxHash:       txHash,
			Receipt:      receipt,
			ReceiptBytes: receiptBytes,
		}

		if idx != nil {
			idx.record(blockNumber, txHash)
		}
	}

	if idx != nil {
		idx.blockBlooms[blockNumber] = blockBloom
	}

	return records
}

// filterName returns a human-readable label for a FilterCriteria.
func filterName(crit filters.FilterCriteria) string {
	hasAddr := len(crit.Addresses) > 0
	hasTopic := len(crit.Topics) > 0 && len(crit.Topics[0]) > 0
	switch {
	case hasAddr && hasTopic:
		return "address+topic0"
	case hasAddr:
		return "address"
	case hasTopic:
		return "topic0"
	default:
		return "none"
	}
}

// pick returns a deterministic index in [0, poolSize) derived from val.
func pick(val uint64, poolSize int) int {
	return int(val % uint64(poolSize)) //nolint:gosec
}
