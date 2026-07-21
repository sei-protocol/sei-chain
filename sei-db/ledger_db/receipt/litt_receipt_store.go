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

	// logIndex is the filtering strategy backed by the pebble index DB:
	// littBloomIndex ("littdb", 16KB per-block blooms) or littTagIndex
	// ("littidx", exact per-tag lookup keys). litt always serves point reads
	// (get-receipt-by-hash via tx-hash secondary keys); only FilterLogs and
	// the index entries differ.
	logIndex    littLogIndex
	backendName string

	latestVersion   atomic.Int64
	earliestVersion atomic.Int64

	keepRecent     int64
	pruneInterval  int64
	stopBackground chan struct{}
	backgroundWg   sync.WaitGroup
	closeOnce      sync.Once
}

var _ ReceiptStore = (*littReceiptStore)(nil)

// littLogIndex is the pluggable filtering index for the litt-backed receipt
// store. litt holds the receipt bodies (point lookup by tx hash); this
// interface owns how a block's logs are indexed for FilterLogs and how those
// index entries are pruned. Implementations must have no false negatives;
// matchLog re-verifies after decode, so correctness never depends on the
// index shape.
type littLogIndex interface {
	stageBlock(s *littReceiptStore, batch dbtypes.Batch, blockNumber uint64, records []ReceiptRecord) error
	filterLogs(s *littReceiptStore, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error)
	// pruneBlocks deletes the index entries for blocks in [floor, cutoff).
	pruneBlocks(s *littReceiptStore, floor, cutoff uint64) error
}

// littBloomIndex stores one 16KB bloom per block ('b' family) and, on query,
// reads every receipt of each candidate block. See block_bloom.go.
type littBloomIndex struct{}

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

func newLittReceiptStore(cfg dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey, logIndex littLogIndex, backendName string) (ReceiptStore, error) {
	if err := os.MkdirAll(cfg.DBDirectory, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create receipt store directory: %w", err)
	}
	littPaths := cfg.LittPaths
	if len(littPaths) == 0 {
		littPaths = []string{filepath.Join(cfg.DBDirectory, "littdb")}
	}
	littConfig, err := litt.DefaultConfig(littPaths...)
	if err != nil {
		return nil, fmt.Errorf("failed to build littdb config: %w", err)
	}
	littConfig.KeymapDirectory = cfg.LittKeymapDirectory
	// Receipt-workload tuning (benchmark-informed). The receipt store is a
	// small-value, many-keys workload (every tx hash is a key), the opposite
	// of litt's few-large-values default. The stock seal triggers fire every
	// few thousand tiny keys / 2MB of key file and stall throughput, so raise
	// every seal cap far past a retention window's worth of writes and let
	// only the value-file size bind. Spread writes across shards (cores spare).
	littConfig.MaxSegmentKeyCount = 100_000_000
	littConfig.TargetSegmentFileSize = 512 << 20
	littConfig.TargetSegmentKeyFileSize = 5 * unit.GB
	littConfig.ShardingFactor = 16
	// The pebble-backed keymap (now opened with options sized for the
	// ~85k random tx-hash key inserts/s this workload funnels through Put;
	// stock options capped littdb at ~82k receipts/s with >2s stalls). The
	// in-memory keymap benchmarks slightly faster still, but holding every
	// live tx hash in memory is not viable at Giga retention windows.
	littConfig.KeymapType = keymap.PebbleDBKeymapType
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
	indexCfg.DataDir = cfg.LogIndexDirectory
	if indexCfg.DataDir == "" {
		indexCfg.DataDir = filepath.Join(cfg.DBDirectory, "log-index")
	}
	index, err := pebbledb.Open(context.Background(), &indexCfg)
	if err != nil {
		_ = values.Close()
		return nil, fmt.Errorf("failed to open receipt log index: %w", err)
	}

	s := &littReceiptStore{
		values:         values,
		receipts:       receipts,
		index:          index,
		logIndex:       logIndex,
		backendName:    backendName,
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
// hashes as secondary keys) and stages the configured log index's entries
// onto the index batch. Repeated calls for the same block append further
// parts; crash replay of an already-persisted part is skipped via the Exists
// check. The next part index is derived by probing litt rather than stored,
// so the index layer holds only what FilterLogs needs.
func (s *littReceiptStore) writeBlock(batch dbtypes.Batch, blockNumber uint64, records []ReceiptRecord) error {
	sortRecordsByTxIndex(records)

	partIndex, err := s.nextPartIndex(blockNumber)
	if err != nil {
		return err
	}

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

	partKey := littPartKey(blockNumber, partIndex)
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
	return s.logIndex.stageBlock(s, batch, blockNumber, records)
}

// nextPartIndex returns the number of parts already written for the block,
// i.e. the index of the next part to write. Probes litt from part 0 until a
// gap; a brand-new block returns 0 after a single Exists. Blocks normally
// have exactly one part, so the common path is one keymap lookup.
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

// stageBlock merges the block's bloom (OR) with any previously stored one,
// preserving no-false-negatives across partial block writes.
func (littBloomIndex) stageBlock(s *littReceiptStore, batch dbtypes.Batch, blockNumber uint64, records []ReceiptRecord) error {
	bloom := buildBlockBloom(records)
	existing, err := s.index.Get(makeBlockPrefixKey(blockNumber))
	if err != nil && !errorutils.IsNotFound(err) {
		return err
	}
	if err == nil {
		if len(existing) != blockBloomSizeBytes {
			return fmt.Errorf("corrupt block bloom entry for block %d: %d bytes", blockNumber, len(existing))
		}
		bloomOr(bloom, existing)
	}
	return batch.Set(makeBlockPrefixKey(blockNumber), bloom)
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
	// Don't warm pruned blocks back into the cache: litt reclaims bodies
	// lazily (TTL), so a pruned block's parts can still be physically present
	// and would otherwise re-enter the cache below the retention floor, where
	// the read-time floor check (which the cache bypasses) can't hide them.
	if earliest := s.earliestVersion.Load(); earliest > 0 && from < uint64(earliest) { //nolint:gosec // earliest is non-negative here
		from = uint64(earliest) //nolint:gosec // earliest is non-negative here
	}

	var records []ReceiptRecord
	for block := from; block <= latestU; block++ {
		partCount, err := s.nextPartIndex(block)
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

// FilterLogs delegates to the configured log index; litt holds the receipt
// bodies and both index strategies have no false negatives, so the results
// are exact after matchLog re-verifies.
func (s *littReceiptStore) FilterLogs(_ sdk.Context, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) > toBlock (%d)", fromBlock, toBlock)
	}
	return s.logIndex.filterLogs(s, fromBlock, toBlock, crit)
}

// filterLogs walks the per-block blooms for the range, skips every block
// whose bloom cannot match, and reads every receipt of each candidate block.
func (littBloomIndex) filterLogs(s *littReceiptStore, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
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
		if len(key) != 1+blockNumLen || len(entry) != blockBloomSizeBytes {
			return nil, fmt.Errorf("corrupt block bloom entry at key %x", key)
		}
		if !bloomMatchesCriteria(entry, crit) {
			continue
		}
		blockNumber := binary.BigEndian.Uint64(key[1:])
		partCount, err := s.nextPartIndex(blockNumber)
		if err != nil {
			return nil, err
		}
		blockLogs, err := s.filterBlockByParts(blockNumber, partCount, crit)
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

// filterBlockByParts reads every receipt of a block (all parts) and applies
// the exact matchLog predicate, numbering logs across the whole block.
func (s *littReceiptStore) filterBlockByParts(blockNumber uint64, partCount uint32, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
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

// pruneBlocksBelow deletes the log index's entries in [earliest, cutoff) and
// advances the retention floor. Receipt values are reclaimed independently by
// litt's TTL GC (whole segments at a time); the read-time floor keeps them
// invisible in the meantime.
func (s *littReceiptStore) pruneBlocksBelow(cutoff uint64) error {
	floor := uint64(0)
	if earliest := s.earliestVersion.Load(); earliest > 0 {
		floor = uint64(earliest) //nolint:gosec // earliest is non-negative here
	}
	if floor >= cutoff {
		return nil
	}

	if err := s.logIndex.pruneBlocks(s, floor, cutoff); err != nil {
		return err
	}
	if err := s.index.Set(receiptEarliestVersionKey, encodeBlockNumber(cutoff), dbtypes.WriteOptions{}); err != nil {
		return err
	}
	s.earliestVersion.Store(int64(cutoff)) //nolint:gosec // block heights fit within int64
	return nil
}

// pruneBlocks deletes the bloom entries for blocks in [floor, cutoff).
func (littBloomIndex) pruneBlocks(s *littReceiptStore, floor, cutoff uint64) error {
	return s.deleteIndexRange(makeBlockPrefixKey(floor), makeBlockPrefixKey(cutoff))
}

// rangeDeleter is implemented by index DBs that can drop a whole key range
// with one range tombstone instead of per-key deletes (pebble).
type rangeDeleter interface {
	DeleteRange(start, end []byte, opts dbtypes.WriteOptions) error
}

// deleteIndexRange removes every index key in [lower, upper). When the index
// DB supports range deletes (pebble) this is a single O(1) range tombstone —
// critical for the tag index, which emits thousands of keys per block, so the
// per-key delete path below would otherwise iterate and delete millions of
// keys per prune pass and starve writes. The iterate-and-batch path is the
// fallback for index DBs without a native range delete.
func (s *littReceiptStore) deleteIndexRange(lower, upper []byte) (err error) {
	if rd, ok := s.index.(rangeDeleter); ok {
		return rd.DeleteRange(lower, upper, dbtypes.WriteOptions{})
	}

	iter, err := s.index.NewIter(&dbtypes.IterOptions{LowerBound: lower, UpperBound: upper})
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := iter.Close(); err == nil {
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
	if count > 0 {
		return batch.Commit(dbtypes.WriteOptions{})
	}
	return nil
}
