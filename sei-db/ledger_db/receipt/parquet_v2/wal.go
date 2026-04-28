package parquet_v2

import (
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

func (c *coordinator) replayWAL(converter WALReceiptConverter) (ReplayResult, error) {
	if converter == nil {
		return ReplayResult{}, fmt.Errorf("WAL receipt converter is nil")
	}
	if c.wal == nil {
		return ReplayResult{}, nil
	}

	firstOffset, errFirst := c.wal.FirstOffset()
	if errFirst != nil || firstOffset <= 0 {
		return ReplayResult{}, nil
	}
	lastOffset, errLast := c.wal.LastOffset()
	if errLast != nil || lastOffset <= 0 {
		return ReplayResult{}, nil
	}

	var (
		currentBlock  uint64
		haveBlock     bool
		logStartIndex uint
		maxBlock      uint64
		dropOffset    uint64
	)

	result := ReplayResult{}
	replayIdx := make(map[uint64]int)

	err := c.wal.Replay(firstOffset, lastOffset, func(offset uint64, entry parquet.WALEntry) error {
		if len(entry.Receipts) == 0 {
			return nil
		}

		blockNumber := entry.BlockNumber
		if blockNumber < c.fileStartBlock {
			dropOffset = offset
			return nil
		}

		if haveBlock && blockNumber != currentBlock && c.isRotationBoundary(blockNumber) && blockNumber > c.fileStartBlock && offset > 0 {
			dropOffset = offset - 1
		}

		if !haveBlock || blockNumber != currentBlock {
			currentBlock = blockNumber
			haveBlock = true
			logStartIndex = 0
		}

		for _, receiptBytes := range entry.Receipts {
			if len(receiptBytes) == 0 {
				continue
			}

			replayed, err := converter(blockNumber, receiptBytes, logStartIndex)
			if err != nil {
				return err
			}
			logStartIndex += replayed.LogCount

			result.WarmupRecords = append(result.WarmupRecords, copyReceiptRecord(replayed.Warmup))
			if idx, ok := replayIdx[blockNumber]; ok {
				result.Blocks[idx].TxHashes = append(result.Blocks[idx].TxHashes, replayed.TxHash)
			} else {
				replayIdx[blockNumber] = len(result.Blocks)
				result.Blocks = append(result.Blocks, ReplayedBlock{
					BlockNumber: blockNumber,
					TxHashes:    []common.Hash{replayed.TxHash},
				})
			}

			input := normalizeReplayInput(blockNumber, receiptBytes, replayed)
			if err := c.applyReceiptFromReplay(input); err != nil {
				return err
			}

			if blockNumber > maxBlock {
				maxBlock = blockNumber
			}
		}

		return nil
	})
	if err != nil {
		return ReplayResult{}, err
	}

	if maxBlock > 0 {
		latest, err := int64FromUint64(maxBlock)
		if err != nil {
			return ReplayResult{}, err
		}
		if latest > c.latestVersion {
			c.latestVersion = latest
		}
	}
	if err := truncateReplayWAL(c.wal, dropOffset); err != nil {
		return ReplayResult{}, err
	}

	c.replayedWarmup = append(c.replayedWarmup[:0], result.WarmupRecords...)
	c.replayedBlocks = append(c.replayedBlocks[:0], result.Blocks...)
	return result, nil
}

func (c *coordinator) applyReceiptFromReplay(input parquet.ReceiptInput) error {
	if c.receiptWriter != nil && input.BlockNumber != c.lastSeenBlock && c.isRotationBoundary(input.BlockNumber) {
		if err := c.rotateOpenFileWithoutWAL(input.BlockNumber); err != nil {
			return err
		}
		c.dropTempCacheBefore(c.fileStartBlock)
	}
	return c.applyReceipt(input)
}

func normalizeReplayInput(blockNumber uint64, receiptBytes []byte, replayed ReplayReceipt) parquet.ReceiptInput {
	input := replayed.Input
	input.BlockNumber = blockNumber
	input.Receipt.BlockNumber = blockNumber
	if len(input.Receipt.TxHash) == 0 {
		input.Receipt.TxHash = append([]byte(nil), replayed.TxHash[:]...)
	}
	if len(input.Receipt.ReceiptBytes) == 0 {
		input.Receipt.ReceiptBytes = append([]byte(nil), receiptBytes...)
	}
	if len(input.ReceiptBytes) == 0 {
		input.ReceiptBytes = append([]byte(nil), receiptBytes...)
	}
	return input
}

func copyReceiptRecord(record parquet.ReceiptRecord) parquet.ReceiptRecord {
	return parquet.ReceiptRecord{
		TxHash:       append([]byte(nil), record.TxHash...),
		BlockNumber:  record.BlockNumber,
		ReceiptBytes: append([]byte(nil), record.ReceiptBytes...),
	}
}

func (c *coordinator) clearWALPreservingLast() error {
	if c.wal == nil {
		return nil
	}
	firstOffset, errFirst := c.wal.FirstOffset()
	if errFirst != nil || firstOffset <= 0 {
		return nil
	}
	lastOffset, errLast := c.wal.LastOffset()
	if errLast != nil || lastOffset <= 0 {
		return nil
	}
	if lastOffset <= firstOffset {
		return nil
	}
	if err := c.wal.TruncateBefore(lastOffset); err != nil {
		if strings.Contains(err.Error(), "out of range") {
			return nil
		}
		return fmt.Errorf("failed to truncate parquet WAL before offset %d: %w", lastOffset, err)
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
