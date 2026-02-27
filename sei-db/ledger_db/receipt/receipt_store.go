package receipt

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// Sentinel errors for consistent error checking.
var (
	ErrNotFound               = errors.New("receipt not found")
	ErrNotConfigured          = errors.New("receipt store not configured")
	ErrRangeQueryNotSupported = errors.New("range query not supported by this backend")
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

type receiptStore struct {
	db          *mvcc.Database
	storeKey    sdk.StoreKey
	stopPruning chan struct{}
	pruneWg     sync.WaitGroup
	closeOnce   sync.Once
}

const (
	receiptBackendPebble  = "pebble"
	receiptBackendParquet = "parquet"
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

func NewReceiptStore(log dbLogger.Logger, config dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey) (ReceiptStore, error) {
	backend, err := newReceiptBackend(log, config, storeKey)
	if err != nil {
		return nil, err
	}
	return newCachedReceiptStore(backend), nil
}

func newReceiptBackend(log dbLogger.Logger, config dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey) (ReceiptStore, error) {
	if log == nil {
		log = dbLogger.NewNopLogger()
	}
	if config.DBDirectory == "" {
		return nil, errors.New("receipt store db directory not configured")
	}

	backend := normalizeReceiptBackend(config.Backend)
	switch backend {
	case receiptBackendParquet:
		return newParquetReceiptStore(log, config, storeKey)
	case receiptBackendPebble:
		ssConfig := dbconfig.DefaultStateStoreConfig()
		ssConfig.DBDirectory = config.DBDirectory
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
		if err := recoverReceiptStore(log, dbutils.GetChangelogPath(ssConfig.DBDirectory), db); err != nil {
			_ = db.Close()
			return nil, err
		}
		rs := &receiptStore{
			db:          db,
			storeKey:    storeKey,
			stopPruning: make(chan struct{}),
		}
		startReceiptPruning(log, db, int64(ssConfig.KeepRecent), int64(ssConfig.PruneIntervalSeconds), rs.stopPruning, &rs.pruneWg)
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
	pairs := make([]*iavl.KVPair, 0, len(receipts))
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
		kvPair := &iavl.KVPair{
			Key:   types.ReceiptKey(record.TxHash),
			Value: marshalledReceipt,
		}
		pairs = append(pairs, kvPair)
	}

	ncs := &proto.NamedChangeSet{
		Name:      types.ReceiptStoreKey,
		Changeset: iavl.ChangeSet{Pairs: pairs},
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

func recoverReceiptStore(log dbLogger.Logger, changelogPath string, db *mvcc.Database) error {
	ssLatestVersion := db.GetLatestVersion()
	log.Info(fmt.Sprintf("Recovering from changelog %s with latest receipt version %d", changelogPath, ssLatestVersion))
	streamHandler, err := wal.NewChangelogWAL(log, changelogPath, wal.Config{})
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
	log.Info(fmt.Sprintf("Start replaying changelog to recover ReceiptStore from offset %d to %d", targetStartOffset, lastOffset))
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

func startReceiptPruning(log dbLogger.Logger, db *mvcc.Database, keepRecent int64, pruneInterval int64, stopCh <-chan struct{}, wg *sync.WaitGroup) {
	if keepRecent <= 0 || pruneInterval <= 0 {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopCh:
				log.Info("Receipt store pruning goroutine stopped")
				return
			default:
			}

			pruneStartTime := time.Now()
			latestVersion := db.GetLatestVersion()
			pruneVersion := latestVersion - keepRecent
			if pruneVersion > 0 {
				// prune all versions up to and including the pruneVersion
				if err := db.Prune(pruneVersion); err != nil {
					log.Error("failed to prune receipt store till", "version", pruneVersion, "err", err)
				}
				log.Info(fmt.Sprintf("Pruned receipt store till version %d took %s\n", pruneVersion, time.Since(pruneStartTime)))
			}

			// Generate a random percentage (between 0% and 100%) of the fixed interval as a delay
			randomPercentage := rand.Float64()
			randomDelay := int64(float64(pruneInterval) * randomPercentage)
			sleepDuration := time.Duration(pruneInterval+randomDelay) * time.Second

			select {
			case <-stopCh:
				log.Info("Receipt store pruning goroutine stopped")
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
