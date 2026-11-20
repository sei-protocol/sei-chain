// Package littidx is the receipt store Giga runs: receipt bodies in LittDB, with a pebble tag
// index for eth_getLogs.
package littidx

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/seilog"

	receiptmetrics "github.com/sei-protocol/sei-chain/sei-db/receipt_db/metrics"

	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	seidbmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
	litttypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/receipt_db/logfilter"
	receipttypes "github.com/sei-protocol/sei-chain/sei-db/receipt_db/types"
	"github.com/sei-protocol/sei-chain/sei-db/receipt_db/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"go.opentelemetry.io/otel"
)

var logger = seilog.NewLogger("db", "receipt-db", "littidx")

// littReceiptStore stores receipt bodies in LittDB and supports eth_getLogs
// via a small pebble tag index (see tag_index.go).
//
// Giga opens this store, and only Giga. It requires every write to be contiguous
// with the head the store opened on — the property Giga establishes by converging
// its stores onto one height at startup — and refuses a write that skips a block
// or lands below the head (see requireNoSkippedBlock and requireNoBlockBelowHead).
//
// Receipt bytes live in litt's immutable append-only segments — large values
// never enter LSM compaction, and expired data is reclaimed by dropping whole
// segments. Each tx hash is a litt secondary key aliasing its receipt's byte
// range, so eth_getTransactionReceipt is one litt lookup with no separate
// index. A block is written as one or more litt "parts" (block + part index ->
// the part's receipts concatenated); a block normally has one part, but legacy
// receipt migration can flush a block across several SetReceipts calls, each
// appending a new immutable part. A block that produced no receipts writes one
// empty part, so every block the store accepted has a key of its own.
//
// The pebble index holds the tag keys (tag_index.go) plus version
// metadata (m:latest / m:earliest).
//
// Durability: litt is flushed every littFlushInterval and index commits do not
// fsync, so after a crash either one may hold blocks the other lost. Startup
// recovery rolls both back to the lower of their heads (see PruneAfter).
//
// Retention: tag keys are pruned by block range, reads enforce the retention
// floor, and receipt bodies are reclaimed once they are both below that floor and
// older than litt's per-table TTL (see gcFilter).
//
// Exactly one driver enforces that retention, selected by cfg.ExternalPruning:
//
//   - unset: the background pruner below keeps the last KeepRecent blocks.
//   - set: the StorageGarbageCollector prunes through the gc.PrunableStore
//     implementation in receipt_gc.go, and startPruning stands down.
//
// Writes are applied in the background, so a receipt is not necessarily readable when SetReceipts
// returns. LatestVersion is the watermark of what has been applied; Close waits for the queue.
type littReceiptStore struct {
	writeMu  sync.Mutex
	values   litt.DB
	receipts litt.Table
	index    dbtypes.KeyValueDB
	storeKey sdk.StoreKey

	latestVersion atomic.Int64
	// stagedVersion is the head the next write follows: latestVersion, or above it by a block whose
	// index commit is still in flight when writes are queued.
	stagedVersion atomic.Int64
	// commitDone is closed once the index commit in flight lands. Guarded by writeMu.
	commitDone      chan struct{}
	earliestVersion atomic.Int64

	keepRecent           int64
	pruneInterval        int64
	externalPruning      bool
	logFilterParallelism int
	// rewardPercentiles is the gas-weighted eth_feeHistory percentile set computed and stored
	// alongside each block's receipts (see ReceiptStoreConfig.RewardPercentiles).
	rewardPercentiles []float64
	stopBackground    chan struct{}
	backgroundWg      sync.WaitGroup
	closeOnce         sync.Once

	// Breaks a write into its stages. Only the writer goroutine records, so one timer serves the store.
	writePhases *seidbmetrics.PhaseTimer

	// Messages waiting for the writer, each a receiptWrite or a receiptWriteBarrier; writeQueue meters time
	// spent waiting on a full queue. A whole write is queued, so the depth is the receipt write's own. Nil means
	// writes apply inline.
	writes chan any

	// Orders admitting a write against shutting the writer down, so none is accepted into a queue
	// that will not be drained. queueWrite holds it shared; Close takes it exclusively.
	admission sync.RWMutex
	closing   bool

	writeQueue   *seidbmetrics.QueueMeter
	writeErr     atomic.Pointer[error]
	stopSampling context.CancelFunc
}

// receiptWrite is one block's receipts, waiting to be applied.
type receiptWrite struct {
	height int64
	// baseFee is the base fee block height ran at, or nil.
	baseFee  *big.Int
	receipts []receipttypes.ReceiptRecord
}

// receiptWriteBarrier is sent by Flush().
type receiptWriteBarrier struct {
	// Closed once every write queued ahead of the barrier is applied.
	done chan struct{}
}

// writeQueueSampleIntervalSeconds is how often the write queue's depth is read. Sampling on a timer
// rather than at each send keeps the reading unbiased by the send rate.
const writeQueueSampleIntervalSeconds = 1

var _ receiptdb.ReceiptStore = (*littReceiptStore)(nil)

var (
	receiptLatestVersionKey   = []byte("m:latest")
	receiptEarliestVersionKey = []byte("m:earliest")
)

const (
	littReceiptTableName = "receipts"
	// littValuesDirName is the store-directory subdirectory holding the littdb
	// receipt bodies.
	littValuesDirName = "littdb"
	// littIndexDirName is the store-directory subdirectory holding the pebble tag
	// index and the store's version metadata.
	littIndexDirName = "log-index"
	// littFlushInterval is roughly one flush per block at Giga throughput (a
	// block is ~7ms), bounding crash loss to about a single block. Flushing this
	// often is only cheap because litt flushes its keymap asynchronously, off the
	// control loop; without that, a per-block flush regresses write throughput
	// badly (≈-48% observed before the async keymap landed).
	littFlushInterval = 5 * time.Millisecond
	// littRetentionTime is litt's per-table TTL: the failsafe minimum age before a
	// receipt body may be reclaimed. It is a flat duration rather than a function of
	// any block count, since how much history this store keeps is decided by the
	// retention floor that gcFilter enforces.
	littRetentionTime = time.Hour

	littPartCountLen = 4
)

// littPartKey identifies one part of a block's receipts in litt. Its 12-byte
// length is disjoint from the 32-byte tx-hash secondary keys.
func littPartKey(blockNumber uint64, part uint32) []byte {
	key := make([]byte, blockNumLen+littPartCountLen)
	binary.BigEndian.PutUint64(key, blockNumber)
	binary.BigEndian.PutUint32(key[blockNumLen:], part)
	return key
}

// gcFilter reports whether litt may reclaim key, making the retention floor a precondition so the
// per-table TTL can only ever reclaim what the floor has already released.
//
// Only primary part keys gate; tx-hash secondaries alias a body in their own segment, so the body's
// part key is what holds that segment back. A floor of 0 means none has been established yet and
// blocks everything.
func (s *littReceiptStore) gcFilter(key []byte, isPrimaryKey bool) (bool, error) {
	if !isPrimaryKey {
		return true, nil
	}
	if len(key) != blockNumLen+littPartCountLen {
		return false, fmt.Errorf("unexpected primary receipt key length %d (want %d)",
			len(key), blockNumLen+littPartCountLen)
	}
	floor := s.earliestVersion.Load()
	if floor <= 0 {
		return false, nil
	}
	return binary.BigEndian.Uint64(key[:blockNumLen]) < uint64(floor), nil //nolint:gosec // guarded positive above
}

// NewReceiptStore opens the receipt store in config.DBDirectory. storeKey names the legacy KV store
// a receipt lookup falls back to, or is nil when there is none.
func NewReceiptStore(config dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey) (receiptdb.ReceiptStore, error) {
	start := time.Now()
	logger.Info("Opening the receipt store")
	if config.DBDirectory == "" {
		return nil, errors.New("receipt store db directory not configured")
	}
	store, err := newLittReceiptStore(config, storeKey)
	if err != nil {
		return nil, fmt.Errorf("open the litt receipt store: %w", err)
	}
	logger.Info("Opened the receipt store", "elapsed", time.Since(start))
	return store, nil
}

func newLittReceiptStore(cfg dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey) (receiptdb.ReceiptStore, error) {
	if err := os.MkdirAll(cfg.DBDirectory, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create receipt store directory: %w", err)
	}
	littConfig, err := litt.DefaultConfig(filepath.Join(cfg.DBDirectory, littValuesDirName))
	if err != nil {
		return nil, fmt.Errorf("failed to build littdb config: %w", err)
	}
	// Receipt-workload tuning: this is a small-value, many-keys workload (every
	// tx hash is a key), the opposite of litt's few-large-values default. The
	// stock seal triggers fire every few thousand tiny keys / 2MB of key file
	// and stall throughput, so raise the key-count and key-file caps past a
	// retention window and let only the value-file size bind segment seals.
	littConfig.MaxSegmentKeyCount = 100_000_000
	littConfig.TargetSegmentFileSize = 512 * unit.MB
	littConfig.TargetSegmentKeyFileSize = 5 * unit.GB
	littConfig.KeymapType = keymap.PebbleDBKeymapType

	// MetricsServeEndpoint stays false: litt records into the process-wide MeterProvider rather than
	// standing up a registry and port of its own.
	littConfig.MetricsEnabled = cfg.LittMetricsEnabled

	values, err := littbuilder.NewDB(littConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to open littdb: %w", err)
	}
	// getLogs per-query block fan-out; non-positive config falls back to the default.
	logFilterParallelism := cfg.LogFilterParallelism
	if logFilterParallelism <= 0 {
		logFilterParallelism = dbconfig.DefaultReceiptLogFilterParallelism
	}
	rewardPercentiles := cfg.RewardPercentiles
	if len(rewardPercentiles) == 0 {
		rewardPercentiles = receiptmetrics.DefaultRewardPercentiles
	}

	// Built before its table, because the table's GC filter is a method on it.
	s := &littReceiptStore{
		values:               values,
		storeKey:             storeKey,
		keepRecent:           int64(cfg.KeepRecent),
		pruneInterval:        int64(cfg.PruneIntervalSeconds),
		externalPruning:      cfg.ExternalPruning,
		logFilterParallelism: logFilterParallelism,
		rewardPercentiles:    rewardPercentiles,
		stopBackground:       make(chan struct{}),
	}

	tableConfig := litt.DefaultTableConfig(littReceiptTableName)
	// single shard: flushing one file is cheaper; sharding mainly helps across multiple disks
	tableConfig.ShardingFactor = 1
	tableConfig.GCFilter = s.gcFilter
	receipts, err := values.BuildTable(tableConfig)
	if err != nil {
		_ = values.Close()
		return nil, fmt.Errorf("failed to open littdb receipts table: %w", err)
	}
	s.receipts = receipts
	// A TTL is necessary for litt to collect at all, and gcFilter makes it insufficient on its own.
	if err := receipts.SetTTL(littRetentionTime); err != nil {
		_ = values.Close()
		return nil, fmt.Errorf("failed to set littdb ttl: %w", err)
	}

	indexCfg := pebbledb.DefaultConfig()
	indexCfg.DataDir = filepath.Join(cfg.DBDirectory, littIndexDirName)
	index, err := pebbledb.Open(context.Background(), &indexCfg)
	if err != nil {
		_ = values.Close()
		return nil, fmt.Errorf("failed to open receipt log index: %w", err)
	}
	s.index = index

	receiptMeter := otel.Meter("seidb_receipt")
	s.writePhases = seidbmetrics.NewPhaseTimer(receiptMeter, "receipt_store_write")
	if cfg.AsyncWriteBuffer > 0 {
		s.writes = make(chan any, cfg.AsyncWriteBuffer)
		s.writeQueue = seidbmetrics.NewQueueMeter(receiptMeter, "receipt_write")
		s.startWriter()

		samplingCtx, stopSampling := context.WithCancel(context.Background())
		s.stopSampling = stopSampling
		s.writeQueue.SampleDepth(samplingCtx, writeQueueSampleIntervalSeconds,
			func() int { return len(s.writes) })
	}

	s.latestVersion.Store(s.readMeta(receiptLatestVersionKey))
	s.stagedVersion.Store(s.latestVersion.Load())
	s.earliestVersion.Store(s.readMeta(receiptEarliestVersionKey))
	s.startPruning()
	s.startFlusher()
	return s, nil
}

func (s *littReceiptStore) readMeta(key []byte) int64 {
	val, err := s.index.Get(key)
	if err != nil || len(val) != blockNumLen {
		return 0
	}
	return int64(binary.BigEndian.Uint64(val)) //nolint:gosec // block heights fit within int64
}

func (s *littReceiptStore) LatestVersion() int64 { return s.latestVersion.Load() }

func (s *littReceiptStore) EarliestVersion() int64 { return s.earliestVersion.Load() }

func (s *littReceiptStore) SetLatestVersion(version int64) error {
	if version <= s.latestVersion.Load() {
		return nil
	}
	height := uint64(version) //nolint:gosec // block heights fit within uint64
	err := s.index.Set(receiptLatestVersionKey, encodeBlockNumber(height), dbtypes.WriteOptions{})
	if err != nil {
		return err
	}
	s.latestVersion.Store(version)
	if version > s.stagedVersion.Load() {
		s.stagedVersion.Store(version)
	}
	return nil
}

func (s *littReceiptStore) SetEarliestVersion(version int64) error {
	// The retention floor only moves forward; never re-expose pruned blocks.
	if version <= s.earliestVersion.Load() {
		return nil
	}
	height := uint64(version) //nolint:gosec // block heights fit within uint64
	err := s.index.Set(receiptEarliestVersionKey, encodeBlockNumber(height), dbtypes.WriteOptions{})
	if err != nil {
		return err
	}
	s.earliestVersion.Store(version)
	return nil
}

func (s *littReceiptStore) GetReceipt(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	stored, err := s.GetReceiptFromStore(ctx, txHash)
	if err == nil {
		return stored, nil
	}
	if !errors.Is(err, receiptdb.ErrNotFound) {
		return nil, err
	}
	// Misses are cheap negatives (one keymap lookup); fall back to the legacy
	// KV store for receipts that predate this store.
	return utils.LegacyReceiptFromKVStore(ctx, s.storeKey, txHash)
}

func (s *littReceiptStore) GetReceiptFromStore(_ sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	val, exists, err := s.receipts.Get(txHash[:])
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, receiptdb.ErrNotFound
	}
	data, err := decodeReceiptData(val)
	if err != nil {
		return nil, err
	}
	var r types.Receipt
	// gogoproto Unmarshal copies all byte/string fields, so it is safe over
	// litt's cache-shared buffer.
	if err := r.Unmarshal(data.Body); err != nil {
		return nil, err
	}
	// Enforce the KeepRecent floor: litt expires values lazily via TTL, so a
	// pruned block may still be physically present.
	if s.belowRetentionFloor(r.BlockNumber) {
		return nil, receiptdb.ErrNotFound
	}
	return &r, nil
}

func (s *littReceiptStore) belowRetentionFloor(blockNumber uint64) bool {
	earliest := s.earliestVersion.Load()
	return earliest > 0 && blockNumber < uint64(earliest) //nolint:gosec // earliest is non-negative
}

// GetBlockStats returns the aggregate stats staged alongside blockNumber's receipts, or
// receiptdb.ErrBlockStatsNotSupported when none was recorded.
func (s *littReceiptStore) GetBlockStats(_ sdk.Context, blockNumber uint64) (receipttypes.BlockStats, error) {
	if s.belowRetentionFloor(blockNumber) {
		return receipttypes.BlockStats{}, receiptdb.ErrNotFound
	}
	val, err := s.index.Get(blockStatsKey(blockNumber))
	if err != nil {
		if errorutils.IsNotFound(err) {
			return receipttypes.BlockStats{}, receiptdb.ErrBlockStatsNotSupported
		}
		return receipttypes.BlockStats{}, err
	}
	stats, err := decodeBlockStats(val)
	if err != nil {
		// A decode failure here reports receiptdb.ErrBlockStatsNotSupported, not the raw error.
		logger.Error("failed to decode block stats, falling back", "block", blockNumber, "err", err)
		return receipttypes.BlockStats{}, receiptdb.ErrBlockStatsNotSupported
	}
	return stats, nil
}

// SetReceipts hands the block's receipts to the writer, blocking only when the queue is full, or
// applies them inline when AsyncWriteBuffer is off. Once a queued write has failed it takes no
// further block and returns that failure.
func (s *littReceiptStore) SetReceipts(ctx sdk.Context, receipts []receipttypes.ReceiptRecord) error {
	baseFee := receiptdb.BlockBaseFee(ctx)
	if err := checkBlockBaseFee(baseFee); err != nil {
		return err
	}
	if baseFee != nil {
		// The write may land after this returns, so it must not share the caller's value.
		baseFee = new(big.Int).Set(baseFee)
	}
	if s.writes == nil {
		return s.applyReceipts(ctx.BlockHeight(), baseFee, receipts)
	}
	if err := s.writeFailure(); err != nil {
		return err
	}
	return s.queueWrite(receiptWrite{height: ctx.BlockHeight(), baseFee: baseFee, receipts: receipts})
}

func (s *littReceiptStore) Flush() error {
	if s.writes != nil {
		done := make(chan struct{})
		if err := s.queueWrite(receiptWriteBarrier{done: done}); err != nil {
			return fmt.Errorf("queue a write barrier: %w", err)
		}
		<-done
	}
	// The last write the writer applied may still be committing its index batch.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.awaitIndexCommit()
}

// ErrStoreClosed is returned by a write the store can no longer apply, the writer having stopped.
var ErrStoreClosed = errors.New("receipt store is closed")

// queueWrite hands a receiptWrite or receiptWriteBarrier to the writer, waiting for room when the queue is full
// and refusing once the store is closing.
func (s *littReceiptStore) queueWrite(message any) error {
	// Held across the send, not merely to read the flag: Close takes it exclusively before stopping
	// the writer, so a write admitted here always reaches a writer that is still running.
	s.admission.RLock()
	defer s.admission.RUnlock()
	if s.closing {
		return ErrStoreClosed
	}
	seidbmetrics.Send(s.writeQueue, s.writes, message)
	return nil
}

// applyReceipts writes a block's receipt bodies, log index and version marker, and reports what it
// committed. The write itself is writeReceipts; this wrapper is where the count is taken, so a
// failed write is not counted as one.
func (s *littReceiptStore) applyReceipts(height int64, baseFee *big.Int, receipts []receipttypes.ReceiptRecord) error {
	written, err := s.writeReceipts(height, baseFee, receipts)
	if err != nil {
		return err
	}
	// The async writer has no request context of its own, so the measurement is unattributed.
	receiptmetrics.RecordReceiptsWritten(context.Background(), written)
	return nil
}

// writeReceipts writes the receipts to litt, then commits their tag keys, block stats and the new
// head to the index in one batch. baseFee, when not nil, is block height's base fee. It returns the
// records it wrote.
func (s *littReceiptStore) writeReceipts(
	height int64,
	baseFee *big.Int,
	receipts []receipttypes.ReceiptRecord,
) ([]receipttypes.ReceiptRecord, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if height < 0 {
		return nil, fmt.Errorf("receipt block height must not be negative: %d", height)
	}

	blockNumbers, receiptsByBlock := groupReceiptRecordsByBlock(receipts)
	if len(blockNumbers) == 0 {
		// A block that produced no receipts is still written, as a part with no receipts in it.
		// The part key is what a walk positions at.
		blockNumbers = []uint64{uint64(height)} //nolint:gosec // height is non-negative
	}
	if err := s.requireNoSkippedBlock(blockNumbers); err != nil {
		return nil, err
	}
	if err := s.requireNoBlockBelowHead(blockNumbers); err != nil {
		return nil, err
	}

	// Closes the stage in flight, so the gap until the next write is charged to neither.
	defer s.writePhases.Reset()

	batch := s.index.NewBatch()
	committing := false
	defer func() {
		if !committing {
			_ = batch.Close()
		}
	}()

	var written []receipttypes.ReceiptRecord
	for _, blockNumber := range blockNumbers {
		var blockBaseFee *big.Int
		if blockNumber == uint64(height) { //nolint:gosec // height is non-negative
			blockBaseFee = baseFee
		}
		blockWritten, err := s.writeBlock(batch, blockNumber, blockBaseFee, receiptsByBlock[blockNumber])
		if err != nil {
			return nil, err
		}
		written = append(written, blockWritten...)
	}

	maxBlock := blockNumbers[len(blockNumbers)-1]
	newLatest := s.stagedVersion.Load()
	if int64(maxBlock) > newLatest { //nolint:gosec // block heights fit within int64
		newLatest = int64(maxBlock) //nolint:gosec // block heights fit within int64
		if err := batch.Set(receiptLatestVersionKey, encodeBlockNumber(maxBlock)); err != nil {
			return nil, err
		}
	}
	// Index commits land in block order, so the previous one lands before this one starts.
	s.writePhases.SetPhase("commit_index")
	if err := s.awaitIndexCommit(); err != nil {
		return nil, err
	}
	if s.writes == nil {
		if err := batch.Commit(dbtypes.WriteOptions{}); err != nil {
			return nil, err
		}
		s.stagedVersion.Store(newLatest)
		s.latestVersion.Store(newLatest)
		return written, nil
	}
	s.stagedVersion.Store(newLatest)
	committing = true
	done := make(chan struct{})
	s.commitDone = done
	go func() {
		defer close(done)
		err := batch.Commit(dbtypes.WriteOptions{})
		_ = batch.Close()
		if err != nil {
			logger.Error("failed to commit receipt log index", "height", maxBlock, "err", err)
			s.writeErr.CompareAndSwap(nil, &err)
			return
		}
		s.latestVersion.Store(newLatest)
	}()
	return written, nil
}

// awaitIndexCommit waits for the index commit in flight, if any, and returns the first write failure.
// The caller holds writeMu.
func (s *littReceiptStore) awaitIndexCommit() error {
	if s.commitDone != nil {
		<-s.commitDone
		s.commitDone = nil
	}
	return s.writeFailure()
}

// writeBlock appends one part for the block into litt (the receipts concatenated, each tx hash a
// secondary key aliasing its receipt's range) and stages the tag keys and the block's stats, with
// baseFee as its base fee, onto the index batch. It returns the records it wrote, which omits
// conditional records that yield to a stored receipt. nextPartIndex picks the next free part slot,
// so a normal block writes part 0 and legacy migration appends.
func (s *littReceiptStore) writeBlock(
	batch dbtypes.Batch,
	blockNumber uint64,
	baseFee *big.Int,
	records []receipttypes.ReceiptRecord,
) ([]receipttypes.ReceiptRecord, error) {
	sortRecordsByTxIndex(records)
	// Check physical keys on the writer, including receipts below the retention
	// floor that LittDB has not collected yet. Reusing those keys is also unsafe.
	records, err := utils.FilterExistingReceipts(records, func(hash common.Hash) (bool, error) {
		return s.receipts.Exists(hash[:])
	})
	if err != nil {
		return nil, err
	}

	s.writePhases.SetPhase("probe_part_index")
	partIndex, err := s.nextPartIndex(blockNumber)
	if err != nil {
		return nil, err
	}
	if err := s.requireNoOrphanedParts(blockNumber, partIndex); err != nil {
		return nil, err
	}

	// The part value is the (metadata-prefixed) receipts concatenated; each tx
	// hash is a secondary key aliasing its receipt's sub-range, which is how
	// every read fetches a receipt — the value is never read whole, so no
	// framing is needed. Each aliased range is a full receiptData record
	// ([version][blockNumber][offset][length][body], see codec.go), so a read
	// recovers both the block-store location of the tx and the receipt body.
	s.writePhases.SetPhase("encode_values")
	value := make([]byte, 0)
	secondaryKeys := make([]*litttypes.SecondaryKey, 0, len(records))
	indexedHashes := make(map[common.Hash]struct{}, len(records))
	for _, record := range records {
		body, err := record.MarshaledReceipt()
		if err != nil {
			return nil, err
		}
		bz := encodeReceiptData(receiptData{
			Header: TxHeader{
				BlockNumber: blockNumber,
				Offset:      record.TxOffset,
				Length:      record.TxLength,
			},
			Body: body,
		})
		partOffset := uint32(len(value)) //nolint:gosec // block regions fit within uint32
		value = append(value, bz...)

		// LittDB requires unique keys within one Put; conditional duplicates were
		// already dropped, so this only guards against repeated unconditional records.
		if _, exists := indexedHashes[record.TxHash]; exists {
			continue
		}
		indexedHashes[record.TxHash] = struct{}{}

		txHash := make([]byte, common.HashLength)
		copy(txHash, record.TxHash[:])
		secondaryKeys = append(secondaryKeys, &litttypes.SecondaryKey{
			Key:    txHash,
			Offset: partOffset,
			Length: uint32(len(bz)), //nolint:gosec // receipt sizes fit within uint32
		})
	}

	s.writePhases.SetPhase("litt_put")
	if err := s.receipts.Put(littPartKey(blockNumber, partIndex), value, secondaryKeys...); err != nil {
		return nil, err
	}

	s.writePhases.SetPhase("stage_tag_keys")
	if err := s.stageTagKeys(batch, blockNumber, records); err != nil {
		return nil, err
	}

	s.writePhases.SetPhase("stage_block_stats")
	if err := s.stageBlockStats(batch, blockNumber, partIndex, baseFee, records); err != nil {
		return nil, err
	}
	return records, nil
}

// stageBlockStats stages the block's aggregate stats, with baseFee as its base fee, onto the index
// batch. A block written across multiple parts (legacy migration) has no aggregate any single call
// can compute, so a later part deletes whatever an earlier part wrote instead.
func (s *littReceiptStore) stageBlockStats(
	batch dbtypes.Batch,
	blockNumber uint64,
	partIndex uint32,
	baseFee *big.Int,
	records []receipttypes.ReceiptRecord,
) error {
	if partIndex > 0 {
		return batch.Delete(blockStatsKey(blockNumber))
	}
	stats := receiptmetrics.ComputeBlockStats(records, s.rewardPercentiles)
	stats.BaseFee = baseFee
	return batch.Set(blockStatsKey(blockNumber), encodeBlockStats(stats))
}

// requireNoSkippedBlock refuses a write that would leave a block unrecorded between the store's
// head and this write. A walk positions at a block's part key, so a block that never reached the
// store makes a walk starting there restart from the oldest receipt. blockNumbers must be sorted
// ascending.
//
// Contiguity is the caller's to maintain, and Giga's startup convergence is what maintains it, so a
// refusal here reports a broken invariant rather than a store that has fallen behind the chain.
func (s *littReceiptStore) requireNoSkippedBlock(blockNumbers []uint64) error {
	head := s.stagedVersion.Load()
	if head <= 0 {
		// Nothing is recorded yet, so this write establishes where the store's history begins.
		return nil
	}
	next := uint64(head) + 1 //nolint:gosec // head is positive
	for _, blockNumber := range blockNumbers {
		if blockNumber > next {
			return fmt.Errorf("receipt write for block %d skips block %d; the store's head is %d",
				blockNumber, next, head)
		}
		if blockNumber >= next {
			next = blockNumber + 1
		}
	}
	return nil
}

// requireNoBlockBelowHead refuses a write for a block below the store's head. Recovery takes the newest
// part in litt as the litt head, which holds only while blocks are written in ascending order.
// blockNumbers must be sorted ascending.
func (s *littReceiptStore) requireNoBlockBelowHead(blockNumbers []uint64) error {
	head := s.stagedVersion.Load()
	if head <= 0 || int64(blockNumbers[0]) >= head { //nolint:gosec // block heights fit within int64
		return nil
	}
	return fmt.Errorf("receipt write for block %d is below the store's head %d", blockNumbers[0], head)
}

// requireNoOrphanedParts returns an error if a block above the store's head already has parts in litt.
// Those parts come from a write whose index commit was lost; another part would store the block twice.
// Startup recovery deletes them.
func (s *littReceiptStore) requireNoOrphanedParts(blockNumber uint64, existingParts uint32) error {
	head := s.stagedVersion.Load()
	if existingParts == 0 || int64(blockNumber) <= head { //nolint:gosec // block heights fit within int64
		return nil
	}
	return fmt.Errorf("receipt block %d already has %d part(s) above the store's head %d; "+
		"restart the node so recovery can remove them", blockNumber, existingParts, head)
}

// nextPartIndex returns the number of parts already written for the block,
// probing litt from part 0 until a gap. A brand-new block returns 0 after one
// Exists; blocks normally have exactly one part.
func (s *littReceiptStore) nextPartIndex(blockNumber uint64) (uint32, error) {
	for part := uint32(0); ; part++ {
		exists, err := s.receipts.Exists(littPartKey(blockNumber, part))
		if err != nil {
			return 0, err
		}
		if !exists {
			return part, nil
		}
	}
}

// FilterLogs answers eth_getLogs via the tag index. Both bounds inclusive; for
// a single block set fromBlock == toBlock.
func (s *littReceiptStore) FilterLogs(
	ctx sdk.Context,
	fromBlock uint64,
	toBlock uint64,
	crit filters.FilterCriteria,
	budget *logfilter.LogBudget,
) ([]*ethtypes.Log, error) {
	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) > toBlock (%d)", fromBlock, toBlock)
	}
	reqCtx := ctx.Context()
	if reqCtx == nil {
		reqCtx = context.Background()
	}
	return s.filterLogsByTags(reqCtx, fromBlock, toBlock, crit, budget)
}

// startWriter applies queued receipt writes in the order they were enqueued, until the store closes.
// It drains what it holds before returning, so a clean shutdown applies them all and an unclean exit
// loses the queue.
func (s *littReceiptStore) startWriter() {
	s.backgroundWg.Add(1)
	go func() {
		defer s.backgroundWg.Done()
		for {
			select {
			case write := <-s.writes:
				s.applyWrite(write)
			case <-s.stopBackground:
				for {
					select {
					case write := <-s.writes:
						s.applyWrite(write)
					default:
						return
					}
				}
			}
		}
	}()
}

// applyWrite handles one queued message, keeping the first write failure for its callers to collect.
// Nothing is applied after a failure: a later block carries its own version marker and would publish
// a head above one whose receipts were never written.
func (s *littReceiptStore) applyWrite(message any) {
	switch m := message.(type) {
	case receiptWriteBarrier:
		close(m.done)
	case receiptWrite:
		if s.writeFailure() != nil {
			return
		}
		if err := s.applyReceipts(m.height, m.baseFee, m.receipts); err != nil {
			logger.Error("failed to write receipts", "height", m.height, "err", err)
			s.writeErr.CompareAndSwap(nil, &err)
		}
	default:
		err := fmt.Errorf("unknown receipt writer message %T", message)
		logger.Error("receipt writer failed", "err", err)
		s.writeErr.CompareAndSwap(nil, &err)
	}
}

// writeFailure returns the first failure a queued write hit. It latches, so every later caller sees
// it rather than the first to ask consuming it.
func (s *littReceiptStore) writeFailure() error {
	if err := s.writeErr.Load(); err != nil {
		return *err
	}
	return nil
}

// startFlusher bounds litt durability lag to littFlushInterval from a
// background goroutine so block commit never waits on an fsync.
func (s *littReceiptStore) startFlusher() {
	s.backgroundWg.Add(1)
	go func() {
		defer s.backgroundWg.Done()
		ticker := time.NewTicker(littFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopBackground:
				return
			case <-ticker.C:
				if err := s.receipts.Flush(); err != nil {
					logger.Error("failed to flush littdb receipts", "err", err)
				}
			}
		}
	}()
}

func (s *littReceiptStore) Close() error {
	var err error
	s.closeOnce.Do(func() {
		// Exclusive and before the writer stops, so it takes effect only once the writes already
		// admitted have been handed over.
		s.admission.Lock()
		s.closing = true
		s.admission.Unlock()

		if s.stopSampling != nil {
			s.stopSampling()
		}
		close(s.stopBackground)
		// The writer drains what it holds before returning, so this is where queued writes land.
		s.backgroundWg.Wait()
		s.writeMu.Lock()
		_ = s.awaitIndexCommit()
		s.writeMu.Unlock()
		err = s.writeFailure()
		// litt's Close flushes, so the last sub-interval of writes is durable.
		if valuesErr := s.values.Close(); err == nil {
			err = valuesErr
		}
		if indexErr := s.index.Close(); err == nil {
			err = indexErr
		}
	})
	return err
}

// runsLocalPruner reports whether this store drives its own retention, which requires that the
// collector is not driving it and that both KeepRecent and PruneIntervalSeconds are set.
func (s *littReceiptStore) runsLocalPruner() bool {
	return !s.ExternalPruning() && s.keepRecent > 0 && s.pruneInterval > 0
}

// startPruning starts the local retention pruner, which keeps the last keepRecent blocks. It does
// nothing unless runsLocalPruner reports true.
func (s *littReceiptStore) startPruning() {
	if !s.runsLocalPruner() {
		return
	}
	s.backgroundWg.Add(1)
	go func() {
		defer s.backgroundWg.Done()
		for {
			// Keep exactly keepRecent blocks, [latest-keepRecent+1, latest]; the
			// +1 matches the pebble backend (which retains keepRecent, not +1).
			pruneBefore := s.latestVersion.Load() - s.keepRecent + 1
			if pruneBefore > 0 {
				if err := s.pruneBlocksBelow(uint64(pruneBefore)); err != nil {
					logger.Error("failed to prune littdb receipt store",
						"before-block", pruneBefore, "err", err)
				}
			}
			// Jittered cadence, matching the other receipt pruners.
			sleep := time.Duration(float64(s.pruneInterval)*(1+rand.Float64())) * time.Second
			select {
			case <-s.stopBackground:
				return
			case <-time.After(sleep):
			}
		}
	}()
}

// pruneBlocksBelow deletes the tag entries in [earliest, cutoff) and advances the retention floor.
// Receipt bodies are released to litt's GC rather than deleted here (see gcFilter), and the read-time
// floor keeps them invisible in the meantime.
//
// It is shared by both retention drivers, startPruning and the collector's PruneHistory. A cutoff
// above this store's head is capped at the head rather than honored.
func (s *littReceiptStore) pruneBlocksBelow(cutoff uint64) error {
	head := s.latestVersion.Load()
	if head <= 0 {
		return nil // nothing ingested, so nothing to drop
	}
	if cutoff > uint64(head) { //nolint:gosec // guarded positive above
		cutoff = uint64(head)
	}

	floor := uint64(0)
	if earliest := s.earliestVersion.Load(); earliest > 0 {
		floor = uint64(earliest) //nolint:gosec // earliest is non-negative
	}
	if floor >= cutoff {
		return nil
	}

	if err := s.deleteIndexRange(littTagBlockKey(floor), littTagBlockKey(cutoff)); err != nil {
		return err
	}
	if err := s.deleteIndexRange(blockStatsKey(floor), blockStatsKey(cutoff)); err != nil {
		return err
	}
	err := s.index.Set(receiptEarliestVersionKey, encodeBlockNumber(cutoff), dbtypes.WriteOptions{})
	if err != nil {
		return err
	}
	s.earliestVersion.Store(int64(cutoff)) //nolint:gosec // block heights fit within int64
	return nil
}

// rangeDeleter is implemented by index DBs that can drop a whole key range with
// one range tombstone instead of per-key deletes (pebble implements it).
type rangeDeleter interface {
	DeleteRange(start, end []byte, opts dbtypes.WriteOptions) error
}

// deleteIndexRange removes every index key in [lower, upper) with one O(1) range
// tombstone — essential for the tag index, which writes thousands of keys per
// block, so per-key deletes would scan and delete millions of keys per prune
// pass. The index is always pebble (which supports range delete); the assertion
// guards against a future backend that does not.
func (s *littReceiptStore) deleteIndexRange(lower, upper []byte) error {
	rd, ok := s.index.(rangeDeleter)
	if !ok {
		return fmt.Errorf("receipt index %T does not support range delete", s.index)
	}
	return rd.DeleteRange(lower, upper, dbtypes.WriteOptions{})
}

// groupReceiptRecordsByBlock splits records by block number (dropping entries
// without a receipt) and returns the block numbers in ascending order.
func groupReceiptRecordsByBlock(receipts []receipttypes.ReceiptRecord) ([]uint64, map[uint64][]receipttypes.ReceiptRecord) {
	byBlock := make(map[uint64][]receipttypes.ReceiptRecord)
	var blockNumbers []uint64
	for _, record := range receipts {
		if record.Receipt == nil {
			continue
		}
		block := record.Receipt.BlockNumber
		if _, ok := byBlock[block]; !ok {
			blockNumbers = append(blockNumbers, block)
		}
		byBlock[block] = append(byBlock[block], record)
	}
	sort.Slice(blockNumbers, func(i, j int) bool { return blockNumbers[i] < blockNumbers[j] })
	return blockNumbers, byBlock
}

// sortRecordsByTxIndex orders a block's records by transaction index (tx hash
// as tiebreaker), the storage order the tag index relies on.
func sortRecordsByTxIndex(records []receipttypes.ReceiptRecord) {
	sort.Slice(records, func(i, j int) bool {
		l, r := records[i].Receipt, records[j].Receipt
		if l.TransactionIndex != r.TransactionIndex {
			return l.TransactionIndex < r.TransactionIndex
		}
		return records[i].TxHash.Cmp(records[j].TxHash) < 0
	})
}
