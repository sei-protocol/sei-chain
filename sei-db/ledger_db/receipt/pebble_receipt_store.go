package receipt

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// pebbleReceiptStore is a ReceiptStore backed by a single block-ordered
// Pebble instance — the all-Pebble alternative to littReceiptStore for
// benchmarking. Receipt values are stored inline under strictly ascending
// keys (the LSM best case: non-overlapping flushes, low compaction
// amplification), every block commits as one atomic batch, and Pebble's WAL
// provides crash recovery.
//
// Key families:
//
//	'H' + txHash (32B)                      -> block (u64 BE) + txIndex (u32 BE)
//	'R' + block (u64 BE) + txIndex (u32 BE) -> receipt bytes
//	'B' + block (u64 BE)                    -> 16KB logs bloom
//	'X' + block (u64 BE) + txHash (32B)     -> nil (reverse index for pruning)
//	m:latest / m:earliest                   -> version metadata
//
// Keying receipt values by transaction index makes SetReceipts idempotent
// and mergeable per block: legacy receipt migration flushes historical
// blocks in tx-hash-ordered subsets, so the same block can be written across
// several calls — distinct transactions land on distinct keys and the stored
// bloom is merged (OR) with each subset rather than replaced.
//
// GetReceipt is two bloom-filtered point reads ('H' then 'R') with cheap
// negatives; FilterLogs and the 'B'/'T' families are delegated to the
// pluggable log index (see ledgerBlockIndex); pruning is one DeleteRange per
// family (plus point deletes of 'H' entries found via 'X').
type pebbleReceiptStore struct {
	db       *pebble.DB
	storeKey sdk.StoreKey
	// index is the log index strategy: bloomBlockIndex ("pebblev3") or
	// tagBlockIndex ("pebbleidx"). Everything else — value layout, hash
	// index, batches, pruning skeleton — is shared, so benchmark differences
	// between the two backends isolate the index design.
	index       ledgerBlockIndex
	backendName string

	latestVersion   atomic.Int64
	earliestVersion atomic.Int64

	keepRecent    int64
	pruneInterval int64
	stopPruning   chan struct{}
	pruneWg       sync.WaitGroup
	closeOnce     sync.Once
}

var _ ReceiptStore = (*pebbleReceiptStore)(nil)

// ledgerBlockIndex is the pluggable log index for the block-ordered pebble
// store: how a block's logs are indexed at write time, how FilterLogs locates
// matching logs, and how index entries are pruned. Implementations must have
// no false negatives; exact matching is re-verified with matchLog after
// decode.
type ledgerBlockIndex interface {
	stageBlock(s *pebbleReceiptStore, batch *pebble.Batch, blockNumber uint64, records []ReceiptRecord) error
	filterLogs(s *pebbleReceiptStore, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error)
	pruneBlocks(batch *pebble.Batch, floor, cutoff uint64) error
}

// bloomBlockIndex is the "pebblev3" log index: one 16KB bloom per block
// ('B' family) built from every log address and topic, used to skip
// non-matching blocks before decoding.
type bloomBlockIndex struct{}

const (
	ledgerHashKeyPrefix    = 'H'
	ledgerBloomKeyPrefix   = 'B'
	ledgerReverseKeyPrefix = 'X'
	ledgerReceiptKeyPrefix = 'R'

	ledgerTxIndexLen = 4
)

func ledgerHashKey(txHash common.Hash) []byte {
	key := make([]byte, 1+txHashLen)
	key[0] = ledgerHashKeyPrefix
	copy(key[1:], txHash[:])
	return key
}

func ledgerBlockKey(prefix byte, blockNumber uint64) []byte {
	key := make([]byte, 1+blockNumLen)
	key[0] = prefix
	binary.BigEndian.PutUint64(key[1:], blockNumber)
	return key
}

func ledgerReverseKey(blockNumber uint64, txHash common.Hash) []byte {
	key := make([]byte, 1+blockNumLen+txHashLen)
	key[0] = ledgerReverseKeyPrefix
	binary.BigEndian.PutUint64(key[1:], blockNumber)
	copy(key[1+blockNumLen:], txHash[:])
	return key
}

func ledgerReceiptKey(blockNumber uint64, txIndex uint32) []byte {
	key := make([]byte, 1+blockNumLen+ledgerTxIndexLen)
	key[0] = ledgerReceiptKeyPrefix
	binary.BigEndian.PutUint64(key[1:], blockNumber)
	binary.BigEndian.PutUint32(key[1+blockNumLen:], txIndex)
	return key
}

// ledgerBlockUpperBound returns the exclusive upper bound covering every key
// with the given prefix up to and including blockNumber.
func ledgerBlockUpperBound(prefix byte, blockNumber uint64) []byte {
	if blockNumber == math.MaxUint64 {
		return []byte{prefix + 1}
	}
	return ledgerBlockKey(prefix, blockNumber+1)
}

func encodeBlockTxIndex(blockNumber uint64, txIndex uint32) []byte {
	buf := make([]byte, blockNumLen+ledgerTxIndexLen)
	binary.BigEndian.PutUint64(buf, blockNumber)
	binary.BigEndian.PutUint32(buf[blockNumLen:], txIndex)
	return buf
}

func decodeBlockTxIndex(val []byte) (uint64, uint32, error) {
	if len(val) != blockNumLen+ledgerTxIndexLen {
		return 0, 0, fmt.Errorf("corrupt receipt hash entry: expected %d bytes, got %d", blockNumLen+ledgerTxIndexLen, len(val))
	}
	return binary.BigEndian.Uint64(val), binary.BigEndian.Uint32(val[blockNumLen:]), nil
}

func newPebbleReceiptStore(cfg dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey, index ledgerBlockIndex, backendName string) (ReceiptStore, error) {
	if err := os.MkdirAll(cfg.DBDirectory, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create receipt store directory: %w", err)
	}
	// The shared tuned options (zstd, bloom filters, pinned format version);
	// the raw *pebble.DB is needed for atomic batches and DeleteRange.
	opts := pebbledb.DefaultPebbleOptions()
	// DefaultPebbleOptions leaves compaction concurrency at pebble's default
	// of 1; receipt ingest at Giga rates is a sustained ~40MB/s of fresh
	// sstables, so allow compactions to scale out under debt instead of
	// stalling ingest behind a single compaction thread.
	opts.CompactionConcurrencyRange = func() (int, int) { return 1, 8 }
	db, err := pebble.Open(cfg.DBDirectory, opts)
	opts.Cache.Unref()
	if err != nil {
		return nil, fmt.Errorf("failed to open receipt ledger pebble db: %w", err)
	}

	s := &pebbleReceiptStore{
		db:            db,
		storeKey:      storeKey,
		index:         index,
		backendName:   backendName,
		keepRecent:    int64(cfg.KeepRecent),
		pruneInterval: int64(cfg.PruneIntervalSeconds),
		stopPruning:   make(chan struct{}),
	}
	s.latestVersion.Store(readLedgerMeta(db, receiptLatestVersionKey))
	s.earliestVersion.Store(readLedgerMeta(db, receiptEarliestVersionKey))
	s.startPruning()
	return s, nil
}

func readLedgerMeta(db *pebble.DB, key []byte) int64 {
	val, closer, err := db.Get(key)
	if err != nil {
		return 0
	}
	defer func() { _ = closer.Close() }()
	if len(val) != blockNumLen {
		return 0
	}
	return int64(binary.BigEndian.Uint64(val)) //nolint:gosec // block heights fit within int64
}

func (s *pebbleReceiptStore) LatestVersion() int64 {
	return s.latestVersion.Load()
}

func (s *pebbleReceiptStore) SetLatestVersion(version int64) error {
	if version <= s.latestVersion.Load() {
		return nil
	}
	if err := s.db.Set(receiptLatestVersionKey, encodeBlockNumber(uint64(version)), pebble.NoSync); err != nil { //nolint:gosec // block heights fit within uint64
		return err
	}
	s.latestVersion.Store(version)
	return nil
}

func (s *pebbleReceiptStore) SetEarliestVersion(version int64) error {
	if err := s.db.Set(receiptEarliestVersionKey, encodeBlockNumber(uint64(version)), pebble.NoSync); err != nil { //nolint:gosec // block heights fit within uint64
		return err
	}
	s.earliestVersion.Store(version)
	return nil
}

func (s *pebbleReceiptStore) GetReceipt(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	receipt, err := s.GetReceiptFromStore(ctx, txHash)
	if err == nil {
		return receipt, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	// Misses are cheap negatives (one bloom-filtered point read); fall back
	// to the legacy KV store for receipts that predate this store.
	return legacyReceiptFromKVStore(ctx, s.storeKey, txHash)
}

func (s *pebbleReceiptStore) GetReceiptFromStore(_ sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	val, closer, err := s.db.Get(ledgerHashKey(txHash))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	blockNumber, txIndex, decodeErr := decodeBlockTxIndex(val)
	_ = closer.Close()
	if decodeErr != nil {
		return nil, decodeErr
	}

	bz, closer, err := s.db.Get(ledgerReceiptKey(blockNumber, txIndex))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var r types.Receipt
	// gogoproto Unmarshal copies all byte/string fields, so the pebble-owned
	// buffer can be released right after.
	unmarshalErr := r.Unmarshal(bz)
	_ = closer.Close()
	if unmarshalErr != nil {
		return nil, unmarshalErr
	}
	return &r, nil
}

func (s *pebbleReceiptStore) SetReceipts(ctx sdk.Context, receipts []ReceiptRecord) error {
	blockNumbers, receiptsByBlock := groupReceiptRecordsByBlock(receipts)
	if len(blockNumbers) == 0 {
		return s.SetLatestVersion(ctx.BlockHeight())
	}

	batch := s.db.NewBatch()
	defer func() { _ = batch.Close() }()

	for _, blockNumber := range blockNumbers {
		if err := s.stageLedgerBlock(batch, blockNumber, receiptsByBlock[blockNumber]); err != nil {
			return err
		}
	}

	maxBlock := blockNumbers[len(blockNumbers)-1]
	newLatest := s.latestVersion.Load()
	if int64(maxBlock) > newLatest { //nolint:gosec // block heights fit within int64
		newLatest = int64(maxBlock) //nolint:gosec // block heights fit within int64
		if err := batch.Set(receiptLatestVersionKey, encodeBlockNumber(maxBlock), nil); err != nil {
			return err
		}
	}

	// One atomic batch per block; Pebble's WAL provides crash recovery (no
	// fsync per block, matching the existing tx-hash index write options).
	if err := batch.Commit(pebble.NoSync); err != nil {
		return err
	}
	s.latestVersion.Store(newLatest)
	return nil
}

// stageLedgerBlock stages one block's hash-index entries, reverse keys,
// log-index entries, and inline receipt values onto the batch. Values are
// keyed by transaction index, so repeated calls for the same block (legacy
// migration subsets, crash replay) merge instead of colliding; the log index
// merges per-block state for the same reason.
func (s *pebbleReceiptStore) stageLedgerBlock(batch *pebble.Batch, blockNumber uint64, records []ReceiptRecord) error {
	sortRecordsByTxIndex(records)

	for _, record := range records {
		receiptBytes, err := marshaledReceipt(record)
		if err != nil {
			return err
		}
		txIndex := record.Receipt.TransactionIndex
		if err := batch.Set(ledgerReceiptKey(blockNumber, txIndex), receiptBytes, nil); err != nil {
			return err
		}
		if err := batch.Set(ledgerHashKey(record.TxHash), encodeBlockTxIndex(blockNumber, txIndex), nil); err != nil {
			return err
		}
		if err := batch.Set(ledgerReverseKey(blockNumber, record.TxHash), nil, nil); err != nil {
			return err
		}
	}

	return s.index.stageBlock(s, batch, blockNumber, records)
}

// stageBlock builds the block's bloom and merges it (OR) with any previously
// stored one, preserving no-false-negatives across partial block writes.
func (bloomBlockIndex) stageBlock(s *pebbleReceiptStore, batch *pebble.Batch, blockNumber uint64, records []ReceiptRecord) error {
	bloom := buildBlockBloom(records)
	if err := s.mergeExistingBloom(blockNumber, bloom); err != nil {
		return err
	}
	return batch.Set(ledgerBlockKey(ledgerBloomKeyPrefix, blockNumber), bloom, nil)
}

// mergeExistingBloom ORs a previously stored bloom for the block (if any)
// into bloom, preserving no-false-negatives across partial block writes.
func (s *pebbleReceiptStore) mergeExistingBloom(blockNumber uint64, bloom []byte) error {
	existing, closer, err := s.db.Get(ledgerBlockKey(ledgerBloomKeyPrefix, blockNumber))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil
		}
		return err
	}
	defer func() { _ = closer.Close() }()
	if len(existing) != blockBloomSizeBytes {
		return fmt.Errorf("corrupt block bloom entry for block %d: %d bytes", blockNumber, len(existing))
	}
	bloomOr(bloom, existing)
	return nil
}

// warmupReceipts loads the current cache chunk's blocks
// ([floor(latest/interval)*interval, latest]) so the cached wrapper's
// coverage window is valid immediately after a restart. Without this, the
// wrapper would answer in-window FilterLogs queries from a cache that never
// saw pre-restart blocks. Implements cacheWarmupProvider.
func (s *pebbleReceiptStore) warmupReceipts() []ReceiptRecord {
	latest := s.latestVersion.Load()
	if latest <= 0 {
		return nil
	}
	latestU := uint64(latest) //nolint:gosec // block heights fit within uint64
	from := (latestU / defaultReceiptCacheRotateInterval) * defaultReceiptCacheRotateInterval

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: ledgerReceiptKey(from, 0),
		UpperBound: ledgerBlockUpperBound(ledgerReceiptKeyPrefix, latestU),
	})
	if err != nil {
		logger.Error("failed to warm receipt cache", "err", err)
		return nil
	}
	defer func() { _ = iter.Close() }()

	var records []ReceiptRecord
	for iter.First(); iter.Valid(); iter.Next() {
		receipt := &types.Receipt{}
		if err := receipt.Unmarshal(iter.Value()); err != nil {
			logger.Error("failed to unmarshal receipt during cache warmup", "err", err)
			continue
		}
		records = append(records, ReceiptRecord{
			TxHash:  common.HexToHash(receipt.TxHashHex),
			Receipt: receipt,
		})
	}
	return records
}

// FilterLogs delegates to the configured log index; both implementations
// have no false negatives and apply the exact matchLog predicate, so the
// results are exact.
func (s *pebbleReceiptStore) FilterLogs(_ sdk.Context, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) > toBlock (%d)", fromBlock, toBlock)
	}
	return s.index.filterLogs(s, fromBlock, toBlock, crit)
}

// filterLogs walks the per-block blooms for the range, skips every block
// whose bloom cannot match, and decodes receipts only for candidate blocks.
func (bloomBlockIndex) filterLogs(s *pebbleReceiptStore, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: ledgerBlockKey(ledgerBloomKeyPrefix, fromBlock),
		UpperBound: ledgerBlockUpperBound(ledgerBloomKeyPrefix, toBlock),
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	var logs []*ethtypes.Log
	for iter.First(); iter.Valid(); iter.Next() {
		entry := iter.Value()
		if len(entry) != blockBloomSizeBytes {
			return nil, fmt.Errorf("corrupt block bloom entry at key %x", iter.Key())
		}
		if !bloomMatchesCriteria(entry, crit) {
			continue
		}
		blockLogs, err := s.filterBlockLogs(binary.BigEndian.Uint64(iter.Key()[1:]), crit)
		if err != nil {
			return nil, err
		}
		logs = append(logs, blockLogs...)
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}
	return logs, nil
}

func (s *pebbleReceiptStore) filterBlockLogs(blockNumber uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: ledgerReceiptKey(blockNumber, 0),
		UpperBound: ledgerBlockUpperBound(ledgerReceiptKeyPrefix, blockNumber),
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	var logs []*ethtypes.Log
	logStartIndex := uint(0)
	for iter.First(); iter.Valid(); iter.Next() {
		receipt := &types.Receipt{}
		if err := receipt.Unmarshal(iter.Value()); err != nil {
			return nil, err
		}
		txLogs := getLogsForTx(receipt, logStartIndex)
		logStartIndex += uint(len(txLogs))
		for _, lg := range txLogs {
			if matchLog(lg, crit) {
				logs = append(logs, lg)
			}
		}
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}
	return logs, nil
}

func (s *pebbleReceiptStore) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.stopPruning)
		s.pruneWg.Wait()
		err = s.db.Close()
	})
	return err
}

func (s *pebbleReceiptStore) startPruning() {
	if s.keepRecent <= 0 || s.pruneInterval <= 0 {
		return
	}
	s.pruneWg.Add(1)
	go func() {
		defer s.pruneWg.Done()
		for {
			pruneBefore := s.latestVersion.Load() - s.keepRecent
			if pruneBefore > 0 {
				start := time.Now()
				if err := s.pruneBlocksBelow(uint64(pruneBefore)); err != nil {
					logger.Error("failed to prune pebble receipt store", "before-block", pruneBefore, "err", err)
				} else {
					logger.Info("Pruned pebble receipt store", "before-block", pruneBefore, "took", time.Since(start))
				}
			}
			// Same jittered cadence as the other receipt pruners.
			sleep := time.Duration(float64(s.pruneInterval)*(1+rand.Float64())) * time.Second
			select {
			case <-s.stopPruning:
				return
			case <-time.After(sleep):
			}
		}
	}()
}

// pruneBlocksBelow removes every block in [earliest, cutoff): hash-index
// point deletes discovered through the reverse keys, then one DeleteRange
// each for the receipt, reverse, and bloom families. Scans start at the
// previous retention floor so each pass only touches the newly pruned
// window. Receipts are immutable and a tx hash maps to exactly one block, so
// the hash-index deletes don't need the re-index ownership check the parquet
// tx-hash index performs.
func (s *pebbleReceiptStore) pruneBlocksBelow(cutoff uint64) error {
	floor := uint64(0)
	if earliest := s.earliestVersion.Load(); earliest > 0 {
		floor = uint64(earliest) //nolint:gosec // earliest is non-negative here
	}
	if floor >= cutoff {
		return nil
	}

	lower := ledgerBlockKey(ledgerReverseKeyPrefix, floor)
	upper := ledgerBlockKey(ledgerReverseKeyPrefix, cutoff)
	iter, err := s.db.NewIter(&pebble.IterOptions{LowerBound: lower, UpperBound: upper})
	if err != nil {
		return err
	}
	defer func() { _ = iter.Close() }()

	const maxBatchDeletes = 10_000
	batch := s.db.NewBatch()
	defer func() { _ = batch.Close() }()
	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		if len(key) != 1+blockNumLen+txHashLen {
			return fmt.Errorf("corrupt receipt reverse-index key %x", key)
		}
		var txHash common.Hash
		copy(txHash[:], key[1+blockNumLen:])
		if err := batch.Delete(ledgerHashKey(txHash), nil); err != nil {
			return err
		}
		count++
		if count >= maxBatchDeletes {
			if err := batch.Commit(pebble.NoSync); err != nil {
				return err
			}
			batch.Reset()
			count = 0
		}
	}
	if err := iter.Error(); err != nil {
		return err
	}

	if err := batch.DeleteRange(ledgerReceiptKey(floor, 0), ledgerBlockKey(ledgerReceiptKeyPrefix, cutoff), nil); err != nil {
		return err
	}
	if err := batch.DeleteRange(lower, upper, nil); err != nil {
		return err
	}
	if err := s.index.pruneBlocks(batch, floor, cutoff); err != nil {
		return err
	}
	if err := batch.Set(receiptEarliestVersionKey, encodeBlockNumber(cutoff), nil); err != nil {
		return err
	}
	if err := batch.Commit(pebble.NoSync); err != nil {
		return err
	}
	s.earliestVersion.Store(int64(cutoff)) //nolint:gosec // block heights fit within int64
	return nil
}

// pruneBlocks removes the bloom entries for blocks in [floor, cutoff).
func (bloomBlockIndex) pruneBlocks(batch *pebble.Batch, floor, cutoff uint64) error {
	return batch.DeleteRange(ledgerBlockKey(ledgerBloomKeyPrefix, floor), ledgerBlockKey(ledgerBloomKeyPrefix, cutoff), nil)
}
