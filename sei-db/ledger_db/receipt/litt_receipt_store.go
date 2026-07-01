package receipt

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
	litttypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// littReceiptStore stores receipt bodies in LittDB and supports eth_getLogs
// via a small pebble tag index (see litt_tag_index.go).
//
// Receipt bytes live in litt's immutable append-only segments — large values
// never enter LSM compaction, and expired data is reclaimed by dropping whole
// segments. Each tx hash is a litt secondary key aliasing its receipt's byte
// range, so eth_getTransactionReceipt is one litt lookup with no separate
// index. A block is written as one or more litt "parts" (block + part index ->
// the part's receipts concatenated); a block normally has one part, but legacy
// receipt migration can flush a block across several SetReceipts calls, each
// appending a new immutable part.
//
// The pebble index holds the tag keys (litt_tag_index.go) plus version
// metadata (m:latest / m:earliest).
//
// Durability: a background flusher bounds litt durability lag to
// littFlushInterval (~one block) without putting fsync on the commit path;
// Close flushes the remainder. A hard crash can lose up to littFlushInterval of
// the most recent receipt bodies while the index still lists them — tolerable
// for auxiliary, non-consensus RPC data, since reads return not-found for a
// missing body. The tight interval is only affordable because litt flushes its
// keymap asynchronously off the control loop. Retention: receipt values
// expire via litt's per-table TTL (time based), tag keys are pruned by block
// range, and reads enforce the KeepRecent floor, so visible retention never
// exceeds KeepRecent regardless of GC timing.
type littReceiptStore struct {
	values   litt.DB
	receipts litt.Table
	index    dbtypes.KeyValueDB
	storeKey sdk.StoreKey

	latestVersion   atomic.Int64
	earliestVersion atomic.Int64

	keepRecent           int64
	pruneInterval        int64
	logFilterParallelism int
	stopBackground       chan struct{}
	backgroundWg         sync.WaitGroup
	closeOnce            sync.Once
}

var _ ReceiptStore = (*littReceiptStore)(nil)

var (
	receiptLatestVersionKey   = []byte("m:latest")
	receiptEarliestVersionKey = []byte("m:earliest")
)

const (
	receiptBackendLittIdx = "littidx"

	littReceiptTableName = "receipts"
	// littFlushInterval is roughly one flush per block at Giga throughput (a
	// block is ~7ms), bounding crash loss to about a single block. Flushing this
	// often is only cheap because litt flushes its keymap asynchronously, off the
	// control loop; without that, a per-block flush regresses write throughput
	// badly (≈-48% observed before the async keymap landed).
	littFlushInterval = 5 * time.Millisecond
	// littTTLPerBlock converts the KeepRecent block count into litt's wall-clock
	// TTL (KeepRecent * littTTLPerBlock). Set above Giga block times so the TTL
	// over-retains; only a sustained block time above this would expire a body
	// still inside the height-based KeepRecent window, which reads mask as
	// not-found (the earliest-version floor is authoritative).
	littTTLPerBlock = 2 * time.Second

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

func newLittReceiptStore(cfg dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey) (ReceiptStore, error) {
	if err := os.MkdirAll(cfg.DBDirectory, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create receipt store directory: %w", err)
	}
	littConfig, err := litt.DefaultConfig(filepath.Join(cfg.DBDirectory, "littdb"))
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

	values, err := littbuilder.NewDB(littConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to open littdb: %w", err)
	}
	tableConfig := litt.DefaultTableConfig(littReceiptTableName)
	tableConfig.ShardingFactor = 1 // single shard: flushing one file is cheaper; sharding mainly helps across multiple disks
	receipts, err := values.BuildTable(tableConfig)
	if err != nil {
		_ = values.Close()
		return nil, fmt.Errorf("failed to open littdb receipts table: %w", err)
	}
	if cfg.KeepRecent > 0 {
		if err := receipts.SetTTL(time.Duration(cfg.KeepRecent) * littTTLPerBlock); err != nil {
			_ = values.Close()
			return nil, fmt.Errorf("failed to set littdb ttl: %w", err)
		}
	}

	indexCfg := pebbledb.DefaultConfig()
	indexCfg.DataDir = filepath.Join(cfg.DBDirectory, "log-index")
	index, err := pebbledb.Open(context.Background(), &indexCfg)
	if err != nil {
		_ = values.Close()
		return nil, fmt.Errorf("failed to open receipt log index: %w", err)
	}

	// getLogs per-query block fan-out; non-positive config falls back to the default.
	logFilterParallelism := cfg.LogFilterParallelism
	if logFilterParallelism <= 0 {
		logFilterParallelism = dbconfig.DefaultReceiptLogFilterParallelism
	}

	s := &littReceiptStore{
		values:               values,
		receipts:             receipts,
		index:                index,
		storeKey:             storeKey,
		keepRecent:           int64(cfg.KeepRecent),
		pruneInterval:        int64(cfg.PruneIntervalSeconds),
		logFilterParallelism: logFilterParallelism,
		stopBackground:       make(chan struct{}),
	}
	s.latestVersion.Store(s.readMeta(receiptLatestVersionKey))
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
	if err := s.index.Set(receiptLatestVersionKey, encodeBlockNumber(uint64(version)), dbtypes.WriteOptions{}); err != nil { //nolint:gosec // block heights fit within uint64
		return err
	}
	s.latestVersion.Store(version)
	return nil
}

func (s *littReceiptStore) SetEarliestVersion(version int64) error {
	// The retention floor only moves forward; never re-expose pruned blocks.
	if version <= s.earliestVersion.Load() {
		return nil
	}
	if err := s.index.Set(receiptEarliestVersionKey, encodeBlockNumber(uint64(version)), dbtypes.WriteOptions{}); err != nil { //nolint:gosec // block heights fit within uint64
		return err
	}
	s.earliestVersion.Store(version)
	return nil
}

func (s *littReceiptStore) GetReceipt(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	receipt, err := s.GetReceiptFromStore(ctx, txHash)
	if err == nil {
		return receipt, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	// Misses are cheap negatives (one keymap lookup); fall back to the legacy
	// KV store for receipts that predate this store.
	return legacyReceiptFromKVStore(ctx, s.storeKey, txHash)
}

func (s *littReceiptStore) GetReceiptFromStore(_ sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	val, exists, err := s.receipts.Get(txHash[:])
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	var r types.Receipt
	// gogoproto Unmarshal copies all byte/string fields, so it is safe over
	// litt's cache-shared buffer.
	if err := r.Unmarshal(val); err != nil {
		return nil, err
	}
	// Enforce the KeepRecent floor: litt expires values lazily via TTL, so a
	// pruned block may still be physically present.
	if s.belowRetentionFloor(r.BlockNumber) {
		return nil, ErrNotFound
	}
	return &r, nil
}

func (s *littReceiptStore) belowRetentionFloor(blockNumber uint64) bool {
	earliest := s.earliestVersion.Load()
	return earliest > 0 && blockNumber < uint64(earliest) //nolint:gosec // earliest is non-negative
}

func (s *littReceiptStore) SetReceipts(ctx sdk.Context, receipts []ReceiptRecord) error {
	blockNumbers, receiptsByBlock := groupReceiptRecordsByBlock(receipts)
	if len(blockNumbers) == 0 {
		return s.SetLatestVersion(ctx.BlockHeight())
	}

	// Receipt values go to litt first; the index batch (tag keys + version
	// meta) commits after, so an indexed block always has its values written.
	batch := s.index.NewBatch()
	defer func() { _ = batch.Close() }()

	for _, blockNumber := range blockNumbers {
		if err := s.writeBlock(batch, blockNumber, receiptsByBlock[blockNumber]); err != nil {
			return err
		}
	}

	maxBlock := blockNumbers[len(blockNumbers)-1]
	newLatest := s.latestVersion.Load()
	if int64(maxBlock) > newLatest { //nolint:gosec // block heights fit within int64
		newLatest = int64(maxBlock) //nolint:gosec // block heights fit within int64
		if err := batch.Set(receiptLatestVersionKey, encodeBlockNumber(maxBlock)); err != nil {
			return err
		}
	}
	if err := batch.Commit(dbtypes.WriteOptions{}); err != nil {
		return err
	}
	s.latestVersion.Store(newLatest)
	return nil
}

// writeBlock appends one part for the block into litt (the receipts
// concatenated, each tx hash a secondary key aliasing its receipt's range) and
// stages the tag keys onto the index batch. nextPartIndex picks the next free
// part slot, so a normal block writes part 0 and legacy migration appends.
func (s *littReceiptStore) writeBlock(batch dbtypes.Batch, blockNumber uint64, records []ReceiptRecord) error {
	sortRecordsByTxIndex(records)

	partIndex, err := s.nextPartIndex(blockNumber)
	if err != nil {
		return err
	}

	// The part value is the receipts concatenated; each tx hash is a secondary
	// key aliasing its receipt's sub-range, which is how every read fetches a
	// receipt — the value is never read whole, so no framing is needed.
	value := make([]byte, 0)
	secondaryKeys := make([]*litttypes.SecondaryKey, 0, len(records))
	for _, record := range records {
		bz, err := marshaledReceipt(record)
		if err != nil {
			return err
		}
		offset := uint32(len(value)) //nolint:gosec // block regions fit within uint32
		value = append(value, bz...)

		txHash := make([]byte, common.HashLength)
		copy(txHash, record.TxHash[:])
		secondaryKeys = append(secondaryKeys, &litttypes.SecondaryKey{
			Key:    txHash,
			Offset: offset,
			Length: uint32(len(bz)), //nolint:gosec // receipt sizes fit within uint32
		})
	}

	if err := s.receipts.Put(littPartKey(blockNumber, partIndex), value, secondaryKeys...); err != nil {
		return err
	}
	return s.stageTagKeys(batch, blockNumber, records)
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
func (s *littReceiptStore) FilterLogs(_ sdk.Context, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) > toBlock (%d)", fromBlock, toBlock)
	}
	return s.filterLogsByTags(fromBlock, toBlock, crit)
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
		close(s.stopBackground)
		s.backgroundWg.Wait()
		// litt's Close flushes, so the last sub-interval of writes is durable.
		err = s.values.Close()
		if indexErr := s.index.Close(); err == nil {
			err = indexErr
		}
	})
	return err
}

func (s *littReceiptStore) startPruning() {
	if s.keepRecent <= 0 || s.pruneInterval <= 0 {
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
					logger.Error("failed to prune littdb receipt store", "before-block", pruneBefore, "err", err)
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

// pruneBlocksBelow deletes the tag entries in [earliest, cutoff) and advances
// the retention floor. Receipt values are reclaimed independently by litt's TTL
// GC; the read-time floor keeps them invisible in the meantime.
func (s *littReceiptStore) pruneBlocksBelow(cutoff uint64) error {
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
	if err := s.index.Set(receiptEarliestVersionKey, encodeBlockNumber(cutoff), dbtypes.WriteOptions{}); err != nil {
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
func groupReceiptRecordsByBlock(receipts []ReceiptRecord) ([]uint64, map[uint64][]ReceiptRecord) {
	byBlock := make(map[uint64][]ReceiptRecord)
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
func sortRecordsByTxIndex(records []ReceiptRecord) {
	sort.Slice(records, func(i, j int) bool {
		l, r := records[i].Receipt, records[j].Receipt
		if l.TransactionIndex != r.TransactionIndex {
			return l.TransactionIndex < r.TransactionIndex
		}
		return records[i].TxHash.Cmp(records[j].TxHash) < 0
	})
}

// marshaledReceipt returns the record's pre-marshaled bytes, marshaling the
// receipt if they were not supplied.
func marshaledReceipt(record ReceiptRecord) ([]byte, error) {
	if len(record.ReceiptBytes) > 0 {
		return record.ReceiptBytes, nil
	}
	return record.Receipt.Marshal()
}

// legacyReceiptFromKVStore looks up a receipt in the legacy KV store for
// receipts that predate this store. Returns ErrNotFound when unavailable.
func legacyReceiptFromKVStore(ctx sdk.Context, storeKey sdk.StoreKey, txHash common.Hash) (*types.Receipt, error) {
	if storeKey == nil {
		return nil, ErrNotFound
	}
	bz := ctx.KVStore(storeKey).Get(types.ReceiptKey(txHash))
	if bz == nil {
		return nil, ErrNotFound
	}
	var r types.Receipt
	if err := r.Unmarshal(bz); err != nil {
		return nil, err
	}
	return &r, nil
}
