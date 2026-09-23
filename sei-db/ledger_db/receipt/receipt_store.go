package receipt

import (
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
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
	// ErrBlockStatsNotSupported is returned by GetBlockStats when the backend never recorded
	// block stats for that block; the caller falls back to summing its receipts.
	ErrBlockStatsNotSupported = errors.New("block stats not supported")
	// ErrTooManyLogs is returned by FilterLogs when a query matches more logs
	// than the caller-supplied limit. It lets callers cap peak memory by
	// aborting a query instead of materializing an unbounded result set.
	ErrTooManyLogs = errors.New("query matches too many logs")
	// ErrTooManyLogBytes is returned when matched logs exceed the byte budget.
	ErrTooManyLogBytes = errors.New("query matches too many log bytes")
)

func NewTooManyLogsError(limit int64) error {
	return fmt.Errorf("%w: result exceeds the maximum of %d logs; narrow the block range or filter criteria", ErrTooManyLogs, limit)
}

func NewTooManyLogBytesError(maxBytes int64) error {
	return fmt.Errorf("%w: result exceeds the maximum of %d bytes; narrow the block range or filter criteria", ErrTooManyLogBytes, maxBytes)
}

// ReceiptStore exposes receipt-specific operations without leaking the StateStore interface.
type ReceiptStore interface {
	controller.PrunableStore

	// LatestVersion is the highest block whose receipts are queryable. A write may land after
	// SetReceipts returns, so a reader follows this rather than the height it last wrote.
	LatestVersion() int64

	EarliestVersion() int64

	GetReceipt(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error)

	GetReceiptFromStore(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error)

	// SetReceipts writes the block's receipts, carrying the version markers with them. An
	// implementation may apply the write in the background; LatestVersion reports when it lands.
	//
	// Every block must be written, one that produced no receipts included; an implementation may
	// refuse a write that skips a block.
	SetReceipts(ctx sdk.Context, receipts []ReceiptRecord) error

	// GetBlockStats returns the aggregate stats recorded when the block's receipts were written.
	// See ErrNotFound and ErrBlockStatsNotSupported.
	GetBlockStats(ctx sdk.Context, blockNumber uint64) (BlockStats, error)

	// FilterLogs queries logs across a range of blocks.
	// For single-block queries, set fromBlock == toBlock.
	// budget is charged per matched log via Reserve and aborts once either
	// configured ceiling is exceeded; nil disables all caps. Callers on the
	// eth range path typically pass a byte-only budget (maxLog=0) here and
	// enforce the matched-log count on the normalized result separately.
	//
	// A store that cannot answer a range query returns ErrRangeQueryNotSupported.
	FilterLogs(
		ctx sdk.Context,
		fromBlock uint64,
		toBlock uint64,
		crit filters.FilterCriteria,
		budget *LogBudget,
	) ([]*ethtypes.Log, error)

	// IterateReceipts walks every receipt stored at or above startBlock, in ascending block
	// order and by transaction index within a block. A startBlock below the oldest receipt the
	// store holds begins at the oldest receipt it holds.
	//
	// A backend that cannot walk its receipts returns
	// ErrRangeQueryNotSupported.
	IterateReceipts(startBlock uint64) (ReceiptIterator, error)

	Close() error
}

// ReceiptIterator walks stored receipts. It is not safe for concurrent use.
type ReceiptIterator interface {
	// Next advances to the next receipt, reporting false once the walk is complete. After it
	// returns false or an error, only Close may be called.
	Next() (bool, error)

	// BlockNumber returns the block holding the current receipt. Valid only after Next
	// returned true.
	BlockNumber() uint64

	// TxHash returns the hash of the current receipt's transaction. Valid only after Next
	// returned true.
	TxHash() common.Hash

	// Receipt decodes the current receipt. Valid only after Next returned true.
	Receipt() (*types.Receipt, error)

	// Close releases the iterator's resources. It MUST be called; failing to do so pins
	// segment files on disk.
	Close() error
}

// VersionPinner is implemented by receipt stores whose version markers can be written directly. It
// is for a caller that put receipts in place by other means and has to state the window they cover.
type VersionPinner interface {
	SetLatestVersion(version int64) error
	SetEarliestVersion(version int64) error
}

// PinVersions widens store's queryable window to [earliest, latest], reporting a store that cannot
// be pinned rather than leaving the window unset.
func PinVersions(store ReceiptStore, earliest, latest int64) error {
	pinner, ok := store.(VersionPinner)
	if !ok {
		return fmt.Errorf("receipt store %T cannot pin versions", store)
	}
	if err := pinner.SetLatestVersion(latest); err != nil {
		return err
	}
	return pinner.SetEarliestVersion(earliest)
}

type ReceiptRecord struct {
	TxHash       common.Hash
	Receipt      *types.Receipt
	ReceiptBytes []byte // Optional pre-marshaled receipt (must match Receipt if set)
	// TxOffset and TxLength locate the raw transaction within its block's stored
	// value in the block store (the sub-range holding this tx). They are written
	// into the receipt value's metadata prefix so a receipt lookup can find the
	// transaction bytes. Only meaningful when block compression is off; zero when
	// unknown.
	TxOffset uint32
	TxLength uint32
	// Reward is this tx's priority fee (EffectiveGasPrice - base fee), or nil to exclude it from
	// the block's reward aggregates.
	Reward *big.Int
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

var _ ReceiptStore = (*receiptStore)(nil)

type receiptStore struct {
	db          seidbtypes.StateStore
	storeKey    sdk.StoreKey
	stopPruning chan struct{}
	pruneWg     sync.WaitGroup
	closeOnce   sync.Once
}

const (
	receiptBackendPebble = "pebble"
)

func normalizeReceiptBackend(backend string) string {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "pebbledb", receiptBackendPebble:
		return receiptBackendPebble
	default:
		return strings.ToLower(strings.TrimSpace(backend))
	}
}

func NewReceiptStore(config dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey) (ReceiptStore, error) {
	return NewReceiptStoreWithReadMetrics(config, storeKey)
}

// NewReceiptStoreWithReadMetrics constructs a receipt store and optionally
// records cache hits, misses, and timings for cached receipt/log reads.
func NewReceiptStoreWithReadMetrics(
	config dbconfig.ReceiptStoreConfig,
	storeKey sdk.StoreKey,
) (ReceiptStore, error) {
	return newReceiptBackend(config, storeKey)
}

// BackendTypeName returns the backend implementation name for testing.
// Returns "" if store is nil or the backend type is unknown.
func BackendTypeName(store ReceiptStore) string {
	if store == nil {
		return ""
	}
	switch store.(type) {
	case *receiptStore:
		return receiptBackendPebble
	case *littReceiptStore:
		return receiptBackendLittIdx
	default:
		return "unknown"
	}
}

func newReceiptBackend(config dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey) (ReceiptStore, error) {
	if config.DBDirectory == "" {
		return nil, errors.New("receipt store db directory not configured")
	}

	backend := normalizeReceiptBackend(config.Backend)
	if err := requireSupportedBackend(backend); err != nil {
		return nil, err
	}
	if backend == receiptBackendPebble && config.ExternalPruning {
		// This backend prunes itself on KeepRecent. Honoring ExternalPruning would stop that
		// pruner with nothing in its place.
		return nil, fmt.Errorf("receipt store backend %q does not support external pruning; use %q",
			receiptBackendPebble, receiptBackendLittIdx)
	}

	// Runs after every config rejection above, and before either backend touches the directory: a config
	// that is about to be rejected must not leave a recorded type behind that then refuses the corrected
	// one.
	if err := recordBackendType(config.DBDirectory, backend); err != nil {
		return nil, err
	}

	switch backend {
	case receiptBackendLittIdx:
		return newLittReceiptStore(config, storeKey)
	case receiptBackendPebble:
		ssConfig := dbconfig.DefaultStateStoreConfig()
		ssConfig.DBDirectory = config.DBDirectory
		ssConfig.AsyncWriteBuffer = config.AsyncWriteBuffer
		ssConfig.KeepRecent = config.KeepRecent
		ssConfig.EnableReadWriteMetrics = config.EnableReadWriteMetrics
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

func (s *receiptStore) EarliestVersion() int64 {
	return s.db.GetEarliestVersion()
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
		return legacyReceiptFromKVStore(ctx, s.storeKey, txHash)
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
	if err := s.applyChangeset(ctx, ncs); err != nil {
		return err
	}
	RecordReceiptsWritten(ctx.Context(), receipts)
	return nil
}

// applyChangeset hands a block's receipt changeset to the state store.
func (s *receiptStore) applyChangeset(ctx sdk.Context, ncs *proto.NamedChangeSet) error {
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
func (s *receiptStore) FilterLogs(_ sdk.Context, _, _ uint64, _ filters.FilterCriteria, _ *LogBudget) ([]*ethtypes.Log, error) {
	return nil, ErrRangeQueryNotSupported
}

// GetBlockStats always reports unsupported: this backend indexes receipts by tx hash only, with
// no per-block grouping to aggregate at write time. Callers fall back to summing receipts.
func (s *receiptStore) GetBlockStats(_ sdk.Context, _ uint64) (BlockStats, error) {
	return BlockStats{}, ErrBlockStatsNotSupported
}

// IterateReceipts is not supported by the pebble backend: receipts are keyed by tx hash, so
// there is no block-ordered walk to offer. Returns ErrRangeQueryNotSupported.
func (s *receiptStore) IterateReceipts(_ uint64) (ReceiptIterator, error) {
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
