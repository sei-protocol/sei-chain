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
	ErrNotFound      = errors.New("receipt not found")
	ErrNotConfigured = errors.New("receipt store not configured")
)

// ReceiptStore exposes receipt-specific operations without leaking the StateStore interface.
type ReceiptStore interface {
	LatestVersion() int64
	SetLatestVersion(version int64) error
	SetEarliestVersion(version int64) error
	GetReceipt(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error)
	GetReceiptFromStore(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error)
	SetReceipts(ctx sdk.Context, receipts []ReceiptRecord) error
	FilterLogs(ctx sdk.Context, blockHeight int64, blockHash common.Hash, txHashes []common.Hash, crit filters.FilterCriteria, applyExactMatch bool) ([]*ethtypes.Log, error)
	Close() error
}

type ReceiptRecord struct {
	TxHash  common.Hash
	Receipt *types.Receipt
}

type receiptStore struct {
	db          *mvcc.Database
	storeKey    sdk.StoreKey
	stopPruning chan struct{}
	closeOnce   sync.Once
}

func normalizeReceiptBackend(backend string) string {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "pebbledb", "pebble":
		return "pebble"
	case "parquet":
		return "parquet"
	default:
		return strings.ToLower(strings.TrimSpace(backend))
	}
}

func NewReceiptStore(log dbLogger.Logger, config dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey) (ReceiptStore, error) {
	if log == nil {
		log = dbLogger.NewNopLogger()
	}
	if config.DBDirectory == "" {
		return nil, errors.New("receipt store db directory not configured")
	}

	backend := normalizeReceiptBackend(config.Backend)
	switch backend {
	case "parquet":
		return newParquetReceiptStore(log, config, storeKey)
	case "pebble":
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
		startReceiptPruning(log, db, int64(ssConfig.KeepRecent), int64(ssConfig.PruneIntervalSeconds), stopPruning)
		return &receiptStore{
			db:          db,
			storeKey:    storeKey,
			stopPruning: stopPruning,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported receipt store backend: %s", config.Backend)
	}
}

func (s *receiptStore) LatestVersion() int64 {
	if s == nil || s.db == nil {
		return 0
	}
	return s.db.GetLatestVersion()
}

func (s *receiptStore) SetLatestVersion(version int64) error {
	if s == nil || s.db == nil {
		return ErrNotConfigured
	}
	return s.db.SetLatestVersion(version)
}

func (s *receiptStore) SetEarliestVersion(version int64) error {
	if s == nil || s.db == nil {
		return ErrNotConfigured
	}
	return s.db.SetEarliestVersion(version, true)
}

func (s *receiptStore) GetReceipt(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	if s == nil || s.db == nil {
		return nil, ErrNotConfigured
	}

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
	if s == nil || s.db == nil {
		return nil, ErrNotConfigured
	}

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
	if s == nil || s.db == nil {
		return ErrNotConfigured
	}

	pairs := make([]*iavl.KVPair, 0, len(receipts))
	for _, record := range receipts {
		if record.Receipt == nil {
			continue
		}
		marshalledReceipt, err := record.Receipt.Marshal()
		if err != nil {
			return err
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

func (s *receiptStore) FilterLogs(ctx sdk.Context, blockHeight int64, blockHash common.Hash, txHashes []common.Hash, crit filters.FilterCriteria, applyExactMatch bool) ([]*ethtypes.Log, error) {
	if s == nil || s.db == nil {
		return nil, ErrNotConfigured
	}
	if len(txHashes) == 0 {
		return []*ethtypes.Log{}, nil
	}

	hasFilters := len(crit.Addresses) != 0 || len(crit.Topics) != 0
	var filterIndexes [][]bloomIndexes
	if hasFilters {
		filterIndexes = encodeFilters(crit.Addresses, crit.Topics)
	}

	logs := make([]*ethtypes.Log, 0)
	totalLogs := uint(0)
	evmTxIndex := 0

	for _, txHash := range txHashes {
		receipt, err := s.GetReceipt(ctx, txHash)
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("collectLogs: unable to find receipt for hash %s", txHash.Hex()))
			continue
		}

		txLogs := getLogsForTx(receipt, totalLogs)

		if hasFilters {
			if len(receipt.LogsBloom) == 0 || matchFilters(ethtypes.Bloom(receipt.LogsBloom), filterIndexes) {
				if applyExactMatch {
					for _, log := range txLogs {
						log.TxIndex = uint(evmTxIndex)        //nolint:gosec
						log.BlockNumber = uint64(blockHeight) //nolint:gosec
						log.BlockHash = blockHash
						if isLogExactMatch(log, crit) {
							logs = append(logs, log)
						}
					}
				} else {
					for _, log := range txLogs {
						log.TxIndex = uint(evmTxIndex)        //nolint:gosec
						log.BlockNumber = uint64(blockHeight) //nolint:gosec
						log.BlockHash = blockHash
						logs = append(logs, log)
					}
				}
			}
		} else {
			for _, log := range txLogs {
				log.TxIndex = uint(evmTxIndex)        //nolint:gosec
				log.BlockNumber = uint64(blockHeight) //nolint:gosec
				log.BlockHash = blockHash
				logs = append(logs, log)
			}
		}

		totalLogs += uint(len(txLogs))
		evmTxIndex++
	}

	return logs, nil
}

func (s *receiptStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	var err error
	s.closeOnce.Do(func() {
		// Signal the pruning goroutine to stop
		if s.stopPruning != nil {
			close(s.stopPruning)
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

func startReceiptPruning(log dbLogger.Logger, db *mvcc.Database, keepRecent int64, pruneInterval int64, stopCh <-chan struct{}) {
	if keepRecent <= 0 || pruneInterval <= 0 {
		return
	}
	go func() {
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
