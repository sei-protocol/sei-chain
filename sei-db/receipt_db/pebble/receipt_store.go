// Package pebble is the receipt store backed by the pebble MVCC state store.
package pebble

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	receiptmetrics "github.com/sei-protocol/sei-chain/sei-db/receipt_db/metrics"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
	"github.com/sei-protocol/sei-chain/sei-db/receipt_db/logfilter"
	receipttypes "github.com/sei-protocol/sei-chain/sei-db/receipt_db/types"
	"github.com/sei-protocol/sei-chain/sei-db/receipt_db/utils"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "receipt-db", "pebble")

var _ receiptdb.ReceiptStore = (*receiptStore)(nil)

type receiptStore struct {
	writeMu     sync.Mutex
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

func NewReceiptStore(config dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey) (receiptdb.ReceiptStore, error) {
	if config.DBDirectory == "" {
		return nil, errors.New("receipt store db directory not configured")
	}

	backend := normalizeReceiptBackend(config.Backend)
	if err := requireSupportedBackend(backend); err != nil {
		return nil, err
	}
	if config.ExternalPruning {
		// This backend prunes itself on KeepRecent. Honoring ExternalPruning would stop that
		// pruner with nothing in its place.
		return nil, fmt.Errorf("receipt store backend %q does not support external pruning",
			receiptBackendPebble)
	}

	// Runs after every config rejection above, and before the backend touches the directory: a config
	// that is about to be rejected must not leave a recorded type behind that then refuses the corrected
	// one.
	if err := recordBackendType(config.DBDirectory, backend); err != nil {
		return nil, err
	}

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
	stored, err := s.GetReceiptFromStore(ctx, txHash)
	if !errors.Is(err, receiptdb.ErrNotFound) {
		return stored, err
	}
	return utils.LegacyReceiptFromKVStore(ctx, s.storeKey, txHash)
}

// GetReceiptFromStore returns the receipt this store holds for txHash, or ErrNotFound, without falling
// back to the legacy KV store.
func (s *receiptStore) GetReceiptFromStore(_ sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	// receipts are immutable, use latest version
	lv := s.db.GetLatestVersion()

	// try persistent store
	bz, err := s.db.Get(types.ReceiptStoreKey, lv, types.ReceiptKey(txHash))
	if err != nil {
		return nil, err
	}
	if bz == nil {
		return nil, receiptdb.ErrNotFound
	}

	var r types.Receipt
	if err := r.Unmarshal(bz); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *receiptStore) SetReceipts(ctx sdk.Context, receipts []receipttypes.ReceiptRecord) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var err error
	drained := false
	receipts, err = utils.FilterExistingReceipts(receipts, func(hash common.Hash) (bool, error) {
		// A conditional read must see every previously queued write.
		if !drained {
			if waiter, ok := s.db.(seidbtypes.PendingWriteWaiter); ok {
				waiter.WaitForPendingWrites()
			}
			drained = true
		}
		value, err := s.db.Get(types.ReceiptStoreKey, s.db.GetLatestVersion(), types.ReceiptKey(hash))
		return value != nil, err
	})
	if err != nil {
		return err
	}
	pairs := make([]*proto.KVPair, 0, len(receipts))
	for _, record := range receipts {
		if record.Receipt == nil {
			continue
		}
		marshalledReceipt, err := record.MarshaledReceipt()
		if err != nil {
			return err
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
	receiptmetrics.RecordReceiptsWritten(ctx.Context(), receipts)
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
func (s *receiptStore) FilterLogs(
	_ sdk.Context,
	_, _ uint64,
	_ filters.FilterCriteria,
	_ *logfilter.LogBudget,
) ([]*ethtypes.Log, error) {
	return nil, receiptdb.ErrRangeQueryNotSupported
}

// GetBlockStats always reports unsupported: this backend indexes receipts by tx hash only, with
// no per-block grouping to aggregate at write time. Callers fall back to summing receipts.
func (s *receiptStore) GetBlockStats(_ sdk.Context, _ uint64) (receipttypes.BlockStats, error) {
	return receipttypes.BlockStats{}, receiptdb.ErrBlockStatsNotSupported
}

// IterateReceipts is not supported by the pebble backend: receipts are keyed by tx hash, so
// there is no block-ordered walk to offer. Returns ErrRangeQueryNotSupported.
func (s *receiptStore) IterateReceipts(_ uint64) (receiptdb.ReceiptIterator, error) {
	return nil, receiptdb.ErrRangeQueryNotSupported
}

func (s *receiptStore) Flush() error {
	if waiter, ok := s.db.(seidbtypes.PendingWriteWaiter); ok {
		waiter.WaitForPendingWrites()
	}
	return nil
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
	defer func() { _ = streamHandler.Close() }()
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
	if lastEntry.Version <= ssLatestVersion {
		return nil
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
	// Replay includes both offsets, so a changelog whose only unapplied entry is the last one replays it.
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
