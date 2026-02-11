package receipt

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// parquetReceiptStore wraps the parquet.Store and implements ReceiptStore.
type parquetReceiptStore struct {
	store    *parquet.Store
	storeKey sdk.StoreKey
}

func newParquetReceiptStore(log dbLogger.Logger, cfg dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey) (ReceiptStore, error) {
	storeCfg := parquet.StoreConfig{
		DBDirectory:          cfg.DBDirectory,
		KeepRecent:           int64(cfg.KeepRecent),
		PruneIntervalSeconds: int64(cfg.PruneIntervalSeconds),
	}

	store, err := parquet.NewStore(log, storeCfg)
	if err != nil {
		return nil, err
	}

	wrapper := &parquetReceiptStore{
		store:    store,
		storeKey: storeKey,
	}

	if err := wrapper.replayWAL(); err != nil {
		return nil, err
	}

	return wrapper, nil
}

func (s *parquetReceiptStore) LatestVersion() int64 {
	return s.store.LatestVersion()
}

func (s *parquetReceiptStore) SetLatestVersion(version int64) error {
	s.store.SetLatestVersion(version)
	return nil
}

func (s *parquetReceiptStore) SetEarliestVersion(version int64) error {
	s.store.SetEarliestVersion(version)
	return nil
}

func (s *parquetReceiptStore) cacheRotateInterval() uint64 {
	return s.store.CacheRotateInterval()
}

func (s *parquetReceiptStore) warmupReceipts() []ReceiptRecord {
	records := make([]ReceiptRecord, 0, len(s.store.WarmupRecords))
	for _, rec := range s.store.WarmupRecords {
		receipt := &types.Receipt{}
		if err := receipt.Unmarshal(rec.ReceiptBytes); err != nil {
			continue
		}
		records = append(records, ReceiptRecord{
			TxHash:  common.BytesToHash(rec.TxHash),
			Receipt: receipt,
		})
	}
	s.store.WarmupRecords = nil
	return records
}

func (s *parquetReceiptStore) GetReceipt(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	result, err := s.store.GetReceiptByTxHash(ctx.Context(), txHash)
	if err != nil {
		return nil, err
	}
	if result != nil {
		receipt := &types.Receipt{}
		if err := receipt.Unmarshal(result.ReceiptBytes); err != nil {
			return nil, err
		}
		return receipt, nil
	}

	store := ctx.KVStore(s.storeKey)
	bz := store.Get(types.ReceiptKey(txHash))
	if bz == nil {
		return nil, ErrNotFound
	}
	var r types.Receipt
	if err := r.Unmarshal(bz); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *parquetReceiptStore) GetReceiptFromStore(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	result, err := s.store.GetReceiptByTxHash(ctx.Context(), txHash)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, ErrNotFound
	}

	receipt := &types.Receipt{}
	if err := receipt.Unmarshal(result.ReceiptBytes); err != nil {
		return nil, err
	}
	return receipt, nil
}

func (s *parquetReceiptStore) SetReceipts(ctx sdk.Context, receipts []ReceiptRecord) error {
	if len(receipts) == 0 {
		if ctx.BlockHeight() > s.store.LatestVersion() {
			s.store.SetLatestVersion(ctx.BlockHeight())
		}
		return nil
	}

	blockHash := common.Hash{}

	inputs := make([]parquet.ReceiptInput, 0, len(receipts))

	var (
		currentBlock  uint64
		logStartIndex uint
		maxBlock      uint64
	)

	for _, record := range receipts {
		if record.Receipt == nil {
			continue
		}

		receipt := record.Receipt
		blockNumber := receipt.BlockNumber
		if blockNumber > maxBlock {
			maxBlock = blockNumber
		}

		if currentBlock == 0 {
			currentBlock = blockNumber
		}
		if blockNumber != currentBlock {
			currentBlock = blockNumber
			logStartIndex = 0
		}

		receiptBytes := record.ReceiptBytes
		if len(receiptBytes) == 0 {
			var err error
			receiptBytes, err = receipt.Marshal()
			if err != nil {
				return err
			}
		}

		txLogs := getLogsForTx(receipt, logStartIndex)
		logStartIndex += uint(len(txLogs))
		for _, lg := range txLogs {
			lg.BlockHash = blockHash
		}

		inputs = append(inputs, parquet.ReceiptInput{
			BlockNumber: blockNumber,
			Receipt: parquet.ReceiptRecord{
				TxHash:       parquet.CopyBytes(record.TxHash[:]),
				BlockNumber:  blockNumber,
				ReceiptBytes: parquet.CopyBytesOrEmpty(receiptBytes),
			},
			Logs:         buildParquetLogRecords(txLogs, blockHash),
			ReceiptBytes: parquet.CopyBytesOrEmpty(receiptBytes),
		})
	}

	if err := s.store.WriteReceipts(inputs); err != nil {
		return err
	}

	if maxBlock > 0 {
		s.store.UpdateLatestVersion(int64(maxBlock)) //nolint:gosec // block numbers won't exceed int64 max
	}

	return nil
}

func (s *parquetReceiptStore) FilterLogs(ctx sdk.Context, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) > toBlock (%d)", fromBlock, toBlock)
	}

	filter := parquet.LogFilter{
		FromBlock: &fromBlock,
		ToBlock:   &toBlock,
		Addresses: crit.Addresses,
		Topics:    crit.Topics,
	}

	results, err := s.store.GetLogs(ctx.Context(), filter)
	if err != nil {
		return nil, err
	}

	logs := make([]*ethtypes.Log, 0, len(results))
	for i := range results {
		lr := results[i]
		logEntry := &ethtypes.Log{
			BlockNumber: lr.BlockNumber,
			TxHash:      common.BytesToHash(lr.TxHash),
			TxIndex:     uint(lr.TxIndex),
			Index:       uint(lr.LogIndex),
			Data:        lr.Data,
			Removed:     lr.Removed,
			BlockHash:   common.BytesToHash(lr.BlockHash),
		}
		copy(logEntry.Address[:], lr.Address)
		logEntry.Topics = buildTopicsFromParquetLogResult(lr)
		logs = append(logs, logEntry)
	}

	return logs, nil
}

func (s *parquetReceiptStore) Close() error {
	return s.store.Close()
}

func (s *parquetReceiptStore) replayWAL() error {
	wal := s.store.WAL()
	if wal == nil {
		return nil
	}

	firstOffset, errFirst := wal.FirstOffset()
	if errFirst != nil || firstOffset <= 0 {
		return nil
	}
	lastOffset, errLast := wal.LastOffset()
	if errLast != nil || lastOffset <= 0 {
		return nil
	}

	var (
		currentBlock  uint64
		logStartIndex uint
		maxBlock      uint64
		dropOffset    uint64
	)

	blockHash := common.Hash{}
	fileStartBlock := s.store.FileStartBlock()

	err := wal.Replay(firstOffset, lastOffset, func(offset uint64, entry parquet.WALEntry) error {
		if len(entry.Receipts) == 0 {
			return nil
		}

		blockNumber := entry.BlockNumber
		if blockNumber < fileStartBlock {
			dropOffset = offset
			return nil
		}

		if currentBlock == 0 {
			currentBlock = blockNumber
		}
		if blockNumber != currentBlock {
			currentBlock = blockNumber
			logStartIndex = 0
		}

		for _, receiptBytes := range entry.Receipts {
			if len(receiptBytes) == 0 {
				continue
			}

			receipt := &types.Receipt{}
			if err := receipt.Unmarshal(receiptBytes); err != nil {
				return err
			}

			txHash := common.HexToHash(receipt.TxHashHex)
			s.store.WarmupRecords = append(s.store.WarmupRecords, parquet.ReceiptRecord{
				TxHash:       parquet.CopyBytes(txHash[:]),
				BlockNumber:  blockNumber,
				ReceiptBytes: parquet.CopyBytesOrEmpty(receiptBytes),
			})

			txLogs := getLogsForTx(receipt, logStartIndex)
			logStartIndex += uint(len(txLogs))
			for _, lg := range txLogs {
				lg.BlockHash = blockHash
			}

			input := parquet.ReceiptInput{
				BlockNumber: blockNumber,
				Receipt: parquet.ReceiptRecord{
					TxHash:       parquet.CopyBytes(txHash[:]),
					BlockNumber:  blockNumber,
					ReceiptBytes: parquet.CopyBytesOrEmpty(receiptBytes),
				},
				Logs: buildParquetLogRecords(txLogs, blockHash),
			}

			if err := s.store.ApplyReceiptFromReplay(input); err != nil {
				return err
			}

			if blockNumber > maxBlock {
				maxBlock = blockNumber
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	if maxBlock > 0 {
		s.store.UpdateLatestVersion(int64(maxBlock)) //nolint:gosec // block numbers won't exceed int64 max
	}
	if err := truncateReplayWAL(wal, dropOffset); err != nil {
		return err
	}
	return nil
}

func truncateReplayWAL(w interface{ TruncateBefore(offset uint64) error }, dropOffset uint64) error {
	if dropOffset == 0 {
		return nil
	}
	if err := w.TruncateBefore(dropOffset + 1); err != nil {
		if strings.Contains(err.Error(), "out of range") {
			return nil
		}
		return fmt.Errorf("failed to truncate replay WAL before offset %d: %w", dropOffset+1, err)
	}
	return nil
}

func buildParquetLogRecords(logs []*ethtypes.Log, blockHash common.Hash) []parquet.LogRecord {
	if len(logs) == 0 {
		return nil
	}

	records := make([]parquet.LogRecord, 0, len(logs))
	for _, lg := range logs {
		topic0, topic1, topic2, topic3 := extractLogTopics(lg.Topics)
		rec := parquet.LogRecord{
			BlockNumber: lg.BlockNumber,
			TxHash:      lg.TxHash[:],
			TxIndex:     parquet.Uint32FromUint(lg.TxIndex),
			LogIndex:    parquet.Uint32FromUint(lg.Index),
			Address:     lg.Address[:],
			BlockHash:   blockHash[:],
			Removed:     lg.Removed,
			Topic0:      topic0,
			Topic1:      topic1,
			Topic2:      topic2,
			Topic3:      topic3,
			Data:        lg.Data,
		}
		records = append(records, rec)
	}

	return records
}

func buildTopicsFromParquetLogResult(lr parquet.LogResult) []common.Hash {
	var topicList []common.Hash
	if len(lr.Topic0) > 0 {
		topicList = append(topicList, common.BytesToHash(lr.Topic0))
	}
	if len(lr.Topic1) > 0 {
		topicList = append(topicList, common.BytesToHash(lr.Topic1))
	}
	if len(lr.Topic2) > 0 {
		topicList = append(topicList, common.BytesToHash(lr.Topic2))
	}
	if len(lr.Topic3) > 0 {
		topicList = append(topicList, common.BytesToHash(lr.Topic3))
	}
	return topicList
}

func extractLogTopics(topics []common.Hash) ([]byte, []byte, []byte, []byte) {
	t0 := make([]byte, 0)
	t1 := make([]byte, 0)
	t2 := make([]byte, 0)
	t3 := make([]byte, 0)

	if len(topics) > 0 {
		t0 = make([]byte, common.HashLength)
		copy(t0, topics[0][:])
	}
	if len(topics) > 1 {
		t1 = make([]byte, common.HashLength)
		copy(t1, topics[1][:])
	}
	if len(topics) > 2 {
		t2 = make([]byte, common.HashLength)
		copy(t2, topics[2][:])
	}
	if len(topics) > 3 {
		t3 = make([]byte, common.HashLength)
		copy(t3, topics[3][:])
	}
	return t0, t1, t2, t3
}
