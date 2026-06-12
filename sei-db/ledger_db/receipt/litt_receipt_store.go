package receipt

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
	litttypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// littReceiptStore is a ReceiptStore backed by LittDB
// (sei-db/db_engine/litt). Receipt bytes live in litt's immutable
// append-only segments, so large values never enter LSM compactions and
// expired data is reclaimed by deleting whole segments.
//
// A block is written as one or more litt "parts". Each part's primary key is
// block number + part index, its value is that part's receipts concatenated
// with u32 length framing, and every tx hash is a litt secondary key
// aliasing its receipt's byte sub-range. The litt keymap therefore IS the
// tx-hash index — eth_getTransactionReceipt is one litt lookup, with no
// separate index to maintain. A block normally has exactly one part; legacy
// receipt migration flushes historical blocks in tx-hash-ordered subsets, so
// the same block can be written across several SetReceipts calls — litt
// values are immutable, so each subset appends a new part instead of
// rewriting the block.
//
// The only state litt cannot hold (it has no iteration and no mutation) sits
// in a small pebble instance via the existing sei-db wrapper:
//
//	'b' + block (u64 BE)  -> partCount (u32 BE) + 16KB logs bloom
//	m:latest / m:earliest -> version metadata
//
// FilterLogs walks the bloom keys for the range, skips blocks whose bloom
// cannot match, fetches only candidate blocks' parts from litt, and applies
// the exact matchLog predicate. Blooms are merged (OR) across partial block
// writes, so they have no false negatives and results are exact.
//
// Retention: receipt values expire via litt's per-table TTL. litt only
// deletes by time, so KeepRecent (a block count) maps to littTTLPerBlock of
// wall time per block. KNOWN LIMITATION: if wall time outpaces block
// production (chain halt, extended node downtime, block times above
// littTTLPerBlock), litt can expire receipts still inside the KeepRecent
// block window; a block-aware GC hook in litt is the deeper fix. Bloom keys
// are pruned by a background loop, and reads enforce the KeepRecent floor,
// so visible retention never exceeds KeepRecent regardless of GC timing.
//
// Durability: a background flusher bounds litt durability lag to
// littFlushInterval without putting fsync on the block-commit path, and
// Close flushes the remainder. A process crash can lose the last
// sub-interval of receipt values while the pebble bloom/meta index (WAL-
// backed) retains them; on replay, writeBlock's Exists check skips parts
// litt already has and re-puts the lost ones. Blocks the application never
// replays stay lost — the same window pebblev3 has with NoSync batches.
type littReceiptStore struct {
	values   litt.DB
	receipts litt.Table
	index    dbtypes.KeyValueDB
	storeKey sdk.StoreKey

	latestVersion   atomic.Int64
	earliestVersion atomic.Int64

	keepRecent     int64
	pruneInterval  int64
	stopBackground chan struct{}
	backgroundWg   sync.WaitGroup
	closeOnce      sync.Once
}

var _ ReceiptStore = (*littReceiptStore)(nil)

const (
	littReceiptTableName = "receipts"
	littFlushInterval    = 100 * time.Millisecond
	// littTTLPerBlock converts the KeepRecent block count into litt's
	// time-based TTL. Chosen well above Giga block times; see the retention
	// note in the type comment for the wall-clock-vs-block-count caveat.
	littTTLPerBlock = 2 * time.Second

	littPartCountLen = 4
)

// littPartKey identifies one part of a block's receipts in litt. The 12-byte
// length is disjoint from 32-byte tx-hash secondary keys.
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
	// Receipt-workload tuning (benchmark-informed). A block contributes
	// ~1 + txCount keys (every tx hash is a secondary key), so litt's 50k
	// default MaxSegmentKeyCount seals a segment every ~50 Giga-sized blocks
	// — sub-second churn whose seal pauses showed up as multi-second p99.9
	// write stalls under unbounded load. Let segment size bind instead, and
	// spread writes across more shards (the node class has cores to spare).
	littConfig.MaxSegmentKeyCount = 2_000_000
	littConfig.TargetSegmentFileSize = 512 << 20
	littConfig.ShardingFactor = 16
	// litt's default keymap is an embedded pebble opened with stock options
	// (4MB memtable, single-threaded compaction — see keymap's
	// pebble.Open(path, &pebble.Options{})), which collapses under the
	// ~85k random tx-hash key inserts/s this workload funnels through Put.
	// The in-memory keymap removes that LSM from the write path entirely;
	// the trade is a full key reload from segment metadata on restart.
	littConfig.KeymapType = keymap.MemKeymapType
	values, err := littbuilder.NewDB(littConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to open littdb: %w", err)
	}
	receipts, err := values.GetTable(littReceiptTableName)
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
	indexCfg.DataDir = filepath.Join(cfg.DBDirectory, "bloom-index")
	index, err := pebbledb.Open(context.Background(), &indexCfg)
	if err != nil {
		_ = values.Close()
		return nil, fmt.Errorf("failed to open receipt bloom index: %w", err)
	}

	s := &littReceiptStore{
		values:         values,
		receipts:       receipts,
		index:          index,
		storeKey:       storeKey,
		keepRecent:     int64(cfg.KeepRecent),
		pruneInterval:  int64(cfg.PruneIntervalSeconds),
		stopBackground: make(chan struct{}),
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

func (s *littReceiptStore) LatestVersion() int64 {
	return s.latestVersion.Load()
}

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
	// Misses are cheap negatives (one keymap lookup); fall back to the
	// legacy KV store for receipts that predate this store.
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
	return earliest > 0 && blockNumber < uint64(earliest) //nolint:gosec // earliest is non-negative here
}

func (s *littReceiptStore) SetReceipts(ctx sdk.Context, receipts []ReceiptRecord) error {
	blockNumbers, receiptsByBlock := groupReceiptRecordsByBlock(receipts)
	if len(blockNumbers) == 0 {
		return s.SetLatestVersion(ctx.BlockHeight())
	}

	// Receipt values go to litt first; the bloom/meta batch commits after, so
	// an indexed block always has its values written (durability of the last
	// flush sub-interval is bounded by the background flusher).
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

// writeBlock appends one part for the block into litt (framed value, tx
// hashes as secondary keys) and stages the merged bloom + part count onto
// the index batch. Repeated calls for the same block append further parts;
// crash replay of an already-persisted part is skipped via the Exists check.
func (s *littReceiptStore) writeBlock(batch dbtypes.Batch, blockNumber uint64, records []ReceiptRecord) error {
	sortRecordsByTxIndex(records)

	partCount, bloomEntry, err := s.blockBloomEntry(blockNumber)
	if err != nil {
		return err
	}
	if bloomEntry == nil {
		bloomEntry = make([]byte, littPartCountLen+blockBloomSizeBytes)
	}
	bloomOr(bloomEntry[littPartCountLen:], buildBlockBloom(records))
	binary.BigEndian.PutUint32(bloomEntry, partCount+1)

	values := make([][]byte, len(records))
	total := 0
	for i, record := range records {
		bz, err := marshaledReceipt(record)
		if err != nil {
			return err
		}
		values[i] = bz
		total += littPartCountLen + len(bz)
	}

	framed := make([]byte, 0, total)
	secondaryKeys := make([]*litttypes.SecondaryKey, 0, len(records))
	for i, bz := range values {
		framed = binary.BigEndian.AppendUint32(framed, uint32(len(bz))) //nolint:gosec // receipt sizes fit within uint32
		offset := uint32(len(framed))                                   //nolint:gosec // block regions fit within uint32
		framed = append(framed, bz...)

		txHash := make([]byte, common.HashLength)
		copy(txHash, records[i].TxHash[:])
		secondaryKeys = append(secondaryKeys, &litttypes.SecondaryKey{
			Key:    txHash,
			Offset: offset,
			Length: uint32(len(bz)), //nolint:gosec // receipt sizes fit within uint32
		})
	}

	partKey := littPartKey(blockNumber, partCount)
	exists, err := s.receipts.Exists(partKey)
	if err != nil {
		return err
	}
	if !exists {
		if err := s.receipts.Put(partKey, framed, secondaryKeys...); err != nil {
			// Synthetic benchmark workloads can repeat a tx hash within a
			// block, which litt rejects up front (duplicate secondary keys in
			// one PutRequest; nothing is written). Real blocks can't contain
			// the same tx twice, so rather than dedupe on the hot path, retry
			// once keeping the first secondary key per hash.
			if !strings.Contains(err.Error(), "duplicate key") {
				return err
			}
			if err := s.receipts.Put(partKey, framed, dedupSecondaryKeys(secondaryKeys)...); err != nil {
				return err
			}
		}
	}
	return batch.Set(makeBlockPrefixKey(blockNumber), bloomEntry)
}

// dedupSecondaryKeys keeps the first secondary key per distinct key bytes.
func dedupSecondaryKeys(keys []*litttypes.SecondaryKey) []*litttypes.SecondaryKey {
	seen := make(map[string]struct{}, len(keys))
	deduped := make([]*litttypes.SecondaryKey, 0, len(keys))
	for _, k := range keys {
		if _, ok := seen[string(k.Key)]; ok {
			continue
		}
		seen[string(k.Key)] = struct{}{}
		deduped = append(deduped, k)
	}
	return deduped
}

// blockBloomEntry returns the stored part count and a mutable copy of the
// full bloom entry for a block, or (0, nil, nil) when the block has no entry.
func (s *littReceiptStore) blockBloomEntry(blockNumber uint64) (uint32, []byte, error) {
	entry, err := s.index.Get(makeBlockPrefixKey(blockNumber))
	if err != nil {
		if errorutils.IsNotFound(err) {
			return 0, nil, nil
		}
		return 0, nil, err
	}
	if len(entry) != littPartCountLen+blockBloomSizeBytes {
		return 0, nil, fmt.Errorf("corrupt block bloom entry for block %d: %d bytes", blockNumber, len(entry))
	}
	return binary.BigEndian.Uint32(entry), entry, nil
}

// blockReceiptValues fetches all parts of a block from litt and splits them
// into per-receipt byte slices, in write order.
func (s *littReceiptStore) blockReceiptValues(blockNumber uint64, partCount uint32) ([][]byte, error) {
	var receipts [][]byte
	for part := uint32(0); part < partCount; part++ {
		region, exists, err := s.receipts.Get(littPartKey(blockNumber, part))
		if err != nil {
			return nil, err
		}
		if !exists {
			// Expired (TTL) or lost to a crash before the flush interval.
			continue
		}
		for cursor := 0; cursor < len(region); {
			if cursor+littPartCountLen > len(region) {
				return nil, fmt.Errorf("corrupt littdb part for block %d at offset %d", blockNumber, cursor)
			}
			length := int(binary.BigEndian.Uint32(region[cursor:]))
			cursor += littPartCountLen
			if cursor+length > len(region) {
				return nil, fmt.Errorf("corrupt littdb part for block %d: receipt length %d overflows region", blockNumber, length)
			}
			receipts = append(receipts, region[cursor:cursor+length])
			cursor += length
		}
	}
	return receipts, nil
}

// warmupReceipts loads the current cache chunk's blocks
// ([floor(latest/interval)*interval, latest]) so the cached wrapper's
// coverage window is valid immediately after a restart. Without this, the
// wrapper would answer in-window FilterLogs queries from a cache that never
// saw pre-restart blocks. Implements cacheWarmupProvider.
func (s *littReceiptStore) warmupReceipts() []ReceiptRecord {
	latest := s.latestVersion.Load()
	if latest <= 0 {
		return nil
	}
	latestU := uint64(latest) //nolint:gosec // block heights fit within uint64
	from := (latestU / defaultReceiptCacheRotateInterval) * defaultReceiptCacheRotateInterval

	var records []ReceiptRecord
	for block := from; block <= latestU; block++ {
		partCount, _, err := s.blockBloomEntry(block)
		if err != nil {
			logger.Error("failed to warm receipt cache", "block", block, "err", err)
			continue
		}
		if partCount == 0 {
			continue
		}
		values, err := s.blockReceiptValues(block, partCount)
		if err != nil {
			logger.Error("failed to warm receipt cache", "block", block, "err", err)
			continue
		}
		for _, bz := range values {
			receipt := &types.Receipt{}
			if err := receipt.Unmarshal(bz); err != nil {
				logger.Error("failed to unmarshal receipt during cache warmup", "block", block, "err", err)
				continue
			}
			records = append(records, ReceiptRecord{
				TxHash:  common.HexToHash(receipt.TxHashHex),
				Receipt: receipt,
			})
		}
	}
	return records
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

// FilterLogs walks the per-block blooms for the range, skips every block
// whose bloom cannot match, fetches receipts only for candidate blocks, and
// applies the exact matchLog predicate. Blooms never produce false
// negatives, so the results are exact.
func (s *littReceiptStore) FilterLogs(_ sdk.Context, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) > toBlock (%d)", fromBlock, toBlock)
	}

	iter, err := s.index.NewIter(&dbtypes.IterOptions{
		LowerBound: makeBlockPrefixKey(fromBlock),
		UpperBound: ledgerBlockUpperBound(blockPrefix, toBlock),
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	var logs []*ethtypes.Log
	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		entry := iter.Value()
		if len(key) != 1+blockNumLen || len(entry) != littPartCountLen+blockBloomSizeBytes {
			return nil, fmt.Errorf("corrupt block bloom entry at key %x", key)
		}
		if !bloomMatchesCriteria(entry[littPartCountLen:], crit) {
			continue
		}
		partCount := binary.BigEndian.Uint32(entry)
		blockLogs, err := s.filterBlockLogs(binary.BigEndian.Uint64(key[1:]), partCount, crit)
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

func (s *littReceiptStore) filterBlockLogs(blockNumber uint64, partCount uint32, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	values, err := s.blockReceiptValues(blockNumber, partCount)
	if err != nil {
		return nil, err
	}

	var logs []*ethtypes.Log
	logStartIndex := uint(0)
	for _, bz := range values {
		receipt := &types.Receipt{}
		if err := receipt.Unmarshal(bz); err != nil {
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
	return logs, nil
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
			pruneBefore := s.latestVersion.Load() - s.keepRecent
			if pruneBefore > 0 {
				if err := s.pruneBlocksBelow(uint64(pruneBefore)); err != nil {
					logger.Error("failed to prune littdb receipt store", "before-block", pruneBefore, "err", err)
				}
			}
			// Same jittered cadence as the other receipt pruners.
			sleep := time.Duration(float64(s.pruneInterval)*(1+rand.Float64())) * time.Second
			select {
			case <-s.stopBackground:
				return
			case <-time.After(sleep):
			}
		}
	}()
}

// pruneBlocksBelow deletes bloom entries in [earliest, cutoff) and advances
// the retention floor. Receipt values are reclaimed independently by litt's
// TTL GC (whole segments at a time); the read-time floor keeps them
// invisible in the meantime.
func (s *littReceiptStore) pruneBlocksBelow(cutoff uint64) (err error) {
	floor := uint64(0)
	if earliest := s.earliestVersion.Load(); earliest > 0 {
		floor = uint64(earliest) //nolint:gosec // earliest is non-negative here
	}
	if floor >= cutoff {
		return nil
	}

	iter, err := s.index.NewIter(&dbtypes.IterOptions{
		LowerBound: makeBlockPrefixKey(floor),
		UpperBound: makeBlockPrefixKey(cutoff),
	})
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := iter.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	batch := s.index.NewBatch()
	defer func() { _ = batch.Close() }()

	const maxBatchDeletes = 10_000
	count := 0
	for ; iter.Valid(); iter.Next() {
		key := make([]byte, len(iter.Key()))
		copy(key, iter.Key())
		if err := batch.Delete(key); err != nil {
			return err
		}
		count++
		if count >= maxBatchDeletes {
			if err := batch.Commit(dbtypes.WriteOptions{}); err != nil {
				return err
			}
			batch.Reset()
			count = 0
		}
	}
	if err := iter.Error(); err != nil {
		return err
	}
	if err := batch.Set(receiptEarliestVersionKey, encodeBlockNumber(cutoff)); err != nil {
		return err
	}
	if err := batch.Commit(dbtypes.WriteOptions{}); err != nil {
		return err
	}
	s.earliestVersion.Store(int64(cutoff)) //nolint:gosec // block heights fit within int64
	return nil
}
