package receipt

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "ledger-db", "receipt")

// Sentinel errors for consistent error checking.
var (
	ErrNotFound               = errors.New("receipt not found")
	ErrNotConfigured          = errors.New("receipt store not configured")
	ErrRangeQueryNotSupported = errors.New("range query not supported by this backend")
	// ErrTxIndexDisabled indicates that a receipt-by-tx-hash lookup missed the
	// in-memory cache and cannot be served because the parquet backend's pebble
	// tx hash index is disabled. A full parquet scan would require reading every
	// file on disk and is intentionally not attempted. The error wraps
	// ErrNotFound so callers that treat "not found" as a null/nil result (e.g.
	// eth_getTransactionReceipt) continue to behave correctly; the wrapping
	// preserves the underlying reason for operators and tests via errors.Is.
	ErrTxIndexDisabled = fmt.Errorf("receipt tx hash index is disabled; parquet fallback scan is not allowed: %w", ErrNotFound)
)

// ReceiptStore exposes receipt-specific operations without leaking the StateStore interface.
type ReceiptStore interface {
	LatestVersion() int64
	SetLatestVersion(version int64) error
	SetEarliestVersion(version int64) error
	GetReceipt(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error)
	GetReceiptFromStore(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error)
	SetReceipts(ctx sdk.Context, receipts []ReceiptRecord) error
	// FilterLogs queries logs across a range of blocks.
	// For single-block queries, set fromBlock == toBlock.
	FilterLogs(ctx sdk.Context, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error)
	Close() error
}

type ReceiptRecord struct {
	TxHash       common.Hash
	Receipt      *types.Receipt
	ReceiptBytes []byte // Optional pre-marshaled receipt (must match Receipt if set)
}

// Version metadata keys shared by the littdb and pebblev3 backends (each in
// its own pebble instance, so the keys never collide).
var (
	receiptLatestVersionKey   = []byte("m:latest")
	receiptEarliestVersionKey = []byte("m:earliest")
)

// groupReceiptRecordsByBlock splits records by block number, dropping entries
// without a receipt. Returns the block numbers in ascending order.
func groupReceiptRecordsByBlock(receipts []ReceiptRecord) ([]uint64, map[uint64][]ReceiptRecord) {
	receiptsByBlock := make(map[uint64][]ReceiptRecord)
	blockNumbers := make([]uint64, 0, 1)
	for _, record := range receipts {
		if record.Receipt == nil {
			continue
		}
		blockNumber := record.Receipt.BlockNumber
		if _, exists := receiptsByBlock[blockNumber]; !exists {
			blockNumbers = append(blockNumbers, blockNumber)
		}
		receiptsByBlock[blockNumber] = append(receiptsByBlock[blockNumber], record)
	}
	sort.Slice(blockNumbers, func(i, j int) bool { return blockNumbers[i] < blockNumbers[j] })
	return blockNumbers, receiptsByBlock
}

// sortRecordsByTxIndex orders a block's records by transaction index (tx hash
// as tiebreaker), the storage order both backends rely on.
func sortRecordsByTxIndex(records []ReceiptRecord) {
	sort.Slice(records, func(i, j int) bool {
		left, right := records[i].Receipt, records[j].Receipt
		if left.TransactionIndex != right.TransactionIndex {
			return left.TransactionIndex < right.TransactionIndex
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
// receipts that predate the persistent receipt store. Returns ErrNotFound
// when the store key is unset or the receipt is absent.
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

// ReceiptReadMetrics records cache hits, misses, and timing for cached receipt
// and log reads.
type ReceiptReadMetrics interface {
	ReportReceiptCacheHit()
	ReportReceiptCacheMiss()
	ReportLogFilterCacheHit()
	ReportLogFilterCacheMiss()
	RecordCacheFilterScanDuration(seconds float64)
	RecordCacheGetDuration(seconds float64)
}

type receiptStore struct {
	db          seidbtypes.StateStore
	storeKey    sdk.StoreKey
	stopPruning chan struct{}
	pruneWg     sync.WaitGroup
	closeOnce   sync.Once
}

const (
	receiptBackendPebble  = "pebble"
	receiptBackendParquet = dbconfig.ReceiptBackendParquet
	// receiptBackendLittDB stores receipt values in LittDB (immutable
	// append-only segments, tx hashes as secondary keys) with a small pebble
	// index for per-block log blooms and version metadata.
	// See litt_receipt_store.go.
	receiptBackendLittDB = dbconfig.ReceiptBackendLittDB
	// receiptBackendPebbleV3 stores receipts inline in a single block-ordered
	// Pebble instance (hash index, per-block blooms, one atomic batch per
	// block). See pebble_receipt_store.go.
	receiptBackendPebbleV3 = dbconfig.ReceiptBackendPebbleV3
	// receiptBackendPebbleIdx is pebblev3 with an exact per-tag lookup index
	// (block/tag/tx keys, intersected per query) in place of the per-block
	// blooms. See pebble_tag_index.go.
	receiptBackendPebbleIdx = dbconfig.ReceiptBackendPebbleIdx
	// receiptBackendLittIdx is littdb (litt point-lookup bodies) with the
	// exact per-tag pebble index instead of per-block blooms — the hybrid:
	// litt for get-receipt-by-hash, pebble tags for getLogs. See
	// litt_tag_index.go.
	receiptBackendLittIdx = dbconfig.ReceiptBackendLittIdx
)

func normalizeReceiptBackend(backend string) string {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "pebbledb", receiptBackendPebble:
		return receiptBackendPebble
	case receiptBackendParquet:
		return receiptBackendParquet
	default:
		return strings.ToLower(strings.TrimSpace(backend))
	}
}

func NewReceiptStore(config dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey) (ReceiptStore, error) {
	return NewReceiptStoreWithReadMetrics(config, storeKey, nil)
}

// NewReceiptStoreWithReadMetrics constructs a receipt store and optionally
// records cache hits, misses, and timings for cached receipt/log reads.
func NewReceiptStoreWithReadMetrics(
	config dbconfig.ReceiptStoreConfig,
	storeKey sdk.StoreKey,
	metrics ReceiptReadMetrics,
) (ReceiptStore, error) {
	backend, err := newReceiptBackend(config, storeKey)
	if err != nil {
		return nil, err
	}
	return newCachedReceiptStore(backend, metrics), nil
}

// BackendTypeName returns the backend implementation name ("parquet" or "pebble") for testing.
// Returns "" if store is nil or the backend type is unknown.
func BackendTypeName(store ReceiptStore) string {
	if store == nil {
		return ""
	}
	if c, ok := store.(*cachedReceiptStore); ok {
		store = c.backend
	}
	switch s := store.(type) {
	case *parquetReceiptStore:
		return receiptBackendParquet
	case *receiptStore:
		return receiptBackendPebble
	case *littReceiptStore:
		return s.backendName
	case *pebbleReceiptStore:
		return s.backendName
	default:
		return "unknown"
	}
}

func newReceiptBackend(config dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey) (ReceiptStore, error) {
	if config.DBDirectory == "" {
		return nil, errors.New("receipt store db directory not configured")
	}

	backend := normalizeReceiptBackend(config.Backend)
	switch backend {
	case receiptBackendParquet:
		return newParquetReceiptStore(config, storeKey)
	case receiptBackendLittDB:
		return newLittReceiptStore(config, storeKey, littBloomIndex{}, receiptBackendLittDB)
	case receiptBackendLittIdx:
		return newLittReceiptStore(config, storeKey, littTagIndex{}, receiptBackendLittIdx)
	case receiptBackendPebbleV3:
		return newPebbleReceiptStore(config, storeKey, bloomBlockIndex{}, receiptBackendPebbleV3)
	case receiptBackendPebbleIdx:
		return newPebbleReceiptStore(config, storeKey, tagBlockIndex{}, receiptBackendPebbleIdx)
	case receiptBackendPebble:
		ssConfig := dbconfig.DefaultStateStoreConfig()
		ssConfig.DBDirectory = config.DBDirectory
		ssConfig.AsyncWriteBuffer = config.AsyncWriteBuffer
		ssConfig.KeepRecent = config.KeepRecent
		if config.PruneIntervalSeconds > 0 {
			ssConfig.PruneIntervalSeconds = config.PruneIntervalSeconds
		}
		ssConfig.KeepLastVersion = false
		ssConfig.Backend = "pebbledb"

		db, err := mvcc.OpenDB(ssConfig.DBDirectory, ssConfig)
		if err != nil {
			return nil, err
		}
		if err := recoverReceiptStore(dbutils.GetChangelogPath(ssConfig.DBDirectory), db); err != nil {
			_ = db.Close()
			return nil, err
		}
		rs := &receiptStore{
			db:          db,
			storeKey:    storeKey,
			stopPruning: make(chan struct{}),
		}
		startReceiptPruning(db, int64(ssConfig.KeepRecent), int64(ssConfig.PruneIntervalSeconds), rs.stopPruning, &rs.pruneWg)
		return rs, nil
	default:
		return nil, fmt.Errorf("unsupported receipt store backend: %s", config.Backend)
	}
}

func (s *receiptStore) LatestVersion() int64 {
	return s.db.GetLatestVersion()
}

func (s *receiptStore) SetLatestVersion(version int64) error {
	return s.db.SetLatestVersion(version)
}

func (s *receiptStore) SetEarliestVersion(version int64) error {
	return s.db.SetEarliestVersion(version, true)
}

func (s *receiptStore) GetReceipt(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	// receipts are immutable, use latest version
	lv := s.db.GetLatestVersion()

	// try persistent store
	bz, err := s.db.Get(types.ReceiptStoreKey, lv, types.ReceiptKey(txHash))
	if err != nil {
		return nil, err
	}

	if bz == nil {
		// try legacy store for older receipts
		store := ctx.KVStore(s.storeKey)
		bz = store.Get(types.ReceiptKey(txHash))
		if bz == nil {
			return nil, ErrNotFound
		}
	}

	var r types.Receipt
	if err := r.Unmarshal(bz); err != nil {
		return nil, err
	}
	return &r, nil
}

// Only used for testing.
func (s *receiptStore) GetReceiptFromStore(_ sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	// receipts are immutable, use latest version
	lv := s.db.GetLatestVersion()

	// try persistent store
	bz, err := s.db.Get(types.ReceiptStoreKey, lv, types.ReceiptKey(txHash))
	if err != nil {
		return nil, err
	}
	if bz == nil {
		return nil, ErrNotFound
	}

	var r types.Receipt
	if err := r.Unmarshal(bz); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *receiptStore) SetReceipts(ctx sdk.Context, receipts []ReceiptRecord) error {
	pairs := make([]*proto.KVPair, 0, len(receipts))
	for _, record := range receipts {
		if record.Receipt == nil {
			continue
		}
		marshalledReceipt := record.ReceiptBytes
		if len(marshalledReceipt) == 0 {
			var err error
			marshalledReceipt, err = record.Receipt.Marshal()
			if err != nil {
				return err
			}
		}
		kvPair := &proto.KVPair{
			Key:   types.ReceiptKey(record.TxHash),
			Value: marshalledReceipt,
		}
		pairs = append(pairs, kvPair)
	}

	ncs := &proto.NamedChangeSet{
		Name:      types.ReceiptStoreKey,
		Changeset: proto.ChangeSet{Pairs: pairs},
	}

	// Genesis and some unit tests execute at block height 0. Async writes
	// rely on a positive version to avoid regressions in the underlying
	// state store metadata, so fall back to a synchronous apply in that case.
	if ctx.BlockHeight() == 0 {
		return s.db.ApplyChangesetSync(ctx.BlockHeight(), []*proto.NamedChangeSet{ncs})
	}

	err := s.db.ApplyChangesetAsync(ctx.BlockHeight(), []*proto.NamedChangeSet{ncs})
	if err != nil {
		if !strings.Contains(err.Error(), "not implemented") { // for tests
			return err
		}
		// fallback to synchronous apply for stores that do not support async writes
		return s.db.ApplyChangesetSync(ctx.BlockHeight(), []*proto.NamedChangeSet{ncs})
	}
	return nil
}

// FilterLogs is not efficiently supported by the pebble backend since receipts
// are indexed by tx hash, not by block number. Returns ErrRangeQueryNotSupported.
// Callers should fall back to fetching receipts individually via GetReceipt.
func (s *receiptStore) FilterLogs(_ sdk.Context, _, _ uint64, _ filters.FilterCriteria) ([]*ethtypes.Log, error) {
	return nil, ErrRangeQueryNotSupported
}

func (s *receiptStore) Close() error {
	var err error
	s.closeOnce.Do(func() {
		if s.stopPruning != nil {
			close(s.stopPruning)
		}
		s.pruneWg.Wait()
		err = s.db.Close()
	})
	return err
}

func recoverReceiptStore(changelogPath string, db seidbtypes.StateStore) error {
	ssLatestVersion := db.GetLatestVersion()
	logger.Info("Recovering from changelog with latest receipt version", "changelog-path", changelogPath, "version", ssLatestVersion)
	streamHandler, err := wal.NewChangelogWAL(changelogPath, wal.Config{})
	if err != nil {
		return err
	}
	firstOffset, errFirst := streamHandler.FirstOffset()
	if firstOffset <= 0 || errFirst != nil {
		return nil
	}
	lastOffset, errLast := streamHandler.LastOffset()
	if lastOffset <= 0 || errLast != nil {
		return nil
	}
	lastEntry, errRead := streamHandler.ReadAt(lastOffset)
	if errRead != nil {
		return errRead
	}
	// Look backward to find where we should start replay from
	curVersion := lastEntry.Version
	curOffset := lastOffset
	if ssLatestVersion > 0 {
		for curVersion > ssLatestVersion && curOffset > firstOffset {
			curOffset--
			curEntry, errRead := streamHandler.ReadAt(curOffset)
			if errRead != nil {
				return errRead
			}
			curVersion = curEntry.Version
		}
	} else {
		// Fresh store (or no applied versions) - start from the first offset
		curOffset = firstOffset
	}
	// Replay from the offset where the version is larger than SS store latest version
	targetStartOffset := curOffset
	logger.Info("Start replaying changelog to recover ReceiptStore", "from-offset", targetStartOffset, "to-offset", lastOffset)
	if targetStartOffset < lastOffset {
		return streamHandler.Replay(targetStartOffset, lastOffset, func(index uint64, entry proto.ChangelogEntry) error {
			// commit to state store
			if err := db.ApplyChangesetSync(entry.Version, entry.Changesets); err != nil {
				return err
			}
			if err := db.SetLatestVersion(entry.Version); err != nil {
				return err
			}
			return nil
		})
	}
	return nil
}

func startReceiptPruning(db seidbtypes.StateStore, keepRecent int64, pruneInterval int64, stopCh <-chan struct{}, wg *sync.WaitGroup) {
	if keepRecent <= 0 || pruneInterval <= 0 {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopCh:
				logger.Info("Receipt store pruning goroutine stopped")
				return
			default:
			}

			pruneStartTime := time.Now()
			latestVersion := db.GetLatestVersion()
			pruneVersion := latestVersion - keepRecent
			if pruneVersion > 0 {
				// prune all versions up to and including the pruneVersion
				if err := db.Prune(pruneVersion); err != nil {
					logger.Error("failed to prune receipt store till", "version", pruneVersion, "err", err)
				}
				logger.Info("Pruned receipt store till version", "version", pruneVersion, "took", time.Since(pruneStartTime))
			}

			// Generate a random percentage (between 0% and 100%) of the fixed interval as a delay
			randomPercentage := rand.Float64()
			randomDelay := int64(float64(pruneInterval) * randomPercentage)
			sleepDuration := time.Duration(pruneInterval+randomDelay) * time.Second

			select {
			case <-stopCh:
				logger.Info("Receipt store pruning goroutine stopped")
				return
			case <-time.After(sleepDuration):
				// Continue to next iteration
			}
		}
	}()
}

func getLogsForTx(receipt *types.Receipt, logStartIndex uint) []*ethtypes.Log {
	return utils.Map(receipt.Logs, func(l *types.Log) *ethtypes.Log { return convertLog(l, receipt, logStartIndex) })
}

func convertLog(l *types.Log, receipt *types.Receipt, logStartIndex uint) *ethtypes.Log {
	return &ethtypes.Log{
		Address:     common.HexToAddress(l.Address),
		Topics:      utils.Map(l.Topics, common.HexToHash),
		Data:        l.Data,
		BlockNumber: receipt.BlockNumber,
		TxHash:      common.HexToHash(receipt.TxHashHex),
		TxIndex:     uint(receipt.TransactionIndex),
		Index:       uint(l.Index) + logStartIndex,
	}
}
