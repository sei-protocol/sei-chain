package receipt

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/filters"
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
	pruneWG     sync.WaitGroup
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
		stopPruning := make(chan struct{})
		rs := &receiptStore{
			db:          db,
			storeKey:    storeKey,
			stopPruning: stopPruning,
		}
		startReceiptPruning(log, db, int64(ssConfig.KeepRecent), int64(ssConfig.PruneIntervalSeconds), stopPruning, &rs.pruneWG)
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
		// Signal the pruning goroutine to stop
		if s.stopPruning != nil {
			close(s.stopPruning)
			s.pruneWG.Wait()
		}
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

var receiptStoreBitMasks = [8]uint8{1, 2, 4, 8, 16, 32, 64, 128}

type bloomIndexes [3]uint

func calcBloomIndexes(b []byte) bloomIndexes {
	b = crypto.Keccak256(b)

	var idxs bloomIndexes
	for i := 0; i < len(idxs); i++ {
		idxs[i] = (uint(b[2*i])<<8)&2047 + uint(b[2*i+1])
	}
	return idxs
}

// res: AND on outer level, OR on mid level, AND on inner level (i.e. all 3 bits)
func encodeFilters(addresses []common.Address, topics [][]common.Hash) (res [][]bloomIndexes) {
	filters := make([][][]byte, 1+len(topics))
	if len(addresses) > 0 {
		filter := make([][]byte, len(addresses))
		for i, address := range addresses {
			filter[i] = address.Bytes()
		}
		filters = append(filters, filter)
	}
	for _, topicList := range topics {
		filter := make([][]byte, len(topicList))
		for i, topic := range topicList {
			filter[i] = topic.Bytes()
		}
		filters = append(filters, filter)
	}
	for _, filter := range filters {
		if len(filter) == 0 {
			continue
		}
		bloomBits := make([]bloomIndexes, len(filter))
		for i, clause := range filter {
			if clause == nil {
				bloomBits = nil
				break
			}
			bloomBits[i] = calcBloomIndexes(clause)
		}
		if bloomBits != nil {
			res = append(res, bloomBits)
		}
	}
	return
}

func matchFilters(bloom ethtypes.Bloom, filters [][]bloomIndexes) bool {
	for _, filter := range filters {
		if !matchFilter(bloom, filter) {
			return false
		}
	}
	return true
}

func matchFilter(bloom ethtypes.Bloom, filter []bloomIndexes) bool {
	for _, possibility := range filter {
		if matchBloomIndexes(bloom, possibility) {
			return true
		}
	}
	return false
}

func matchBloomIndexes(bloom ethtypes.Bloom, idx bloomIndexes) bool {
	for _, bit := range idx {
		// big endian
		whichByte := bloom[ethtypes.BloomByteLength-1-bit/8]
		mask := receiptStoreBitMasks[bit%8]
		if whichByte&mask == 0 {
			return false
		}
	}
	return true
}

func isLogExactMatch(log *ethtypes.Log, crit filters.FilterCriteria) bool {
	addrMatch := len(crit.Addresses) == 0
	for _, addrFilter := range crit.Addresses {
		if log.Address == addrFilter {
			addrMatch = true
			break
		}
	}
	return addrMatch && matchTopics(crit.Topics, log.Topics)
}

func matchTopics(topics [][]common.Hash, eventTopics []common.Hash) bool {
	for i, topicList := range topics {
		if len(topicList) == 0 {
			continue
		}
		if i >= len(eventTopics) {
			return false
		}
		matched := false
		for _, topic := range topicList {
			if topic == eventTopics[i] {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
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
