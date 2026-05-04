package coordinator

import (
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

// replayWAL re-applies WAL entries on top of the on-disk parquet state. It
// drives rotation when entries cross a MaxBlocksPerFile boundary (so the
// resulting layout matches what a non-crashing run would have produced),
// applies each receipt to the open writer, and finally truncates WAL
// entries that are now durably persisted.
func (c *Coordinator) replayWAL(converter WALReceiptConverter) (ReplayResult, error) {
	if converter == nil {
		return ReplayResult{}, fmt.Errorf("WAL receipt converter is nil")
	}
	if c.wal == nil {
		return ReplayResult{}, nil
	}

	firstOffset, err := c.wal.FirstOffset()
	if err != nil {
		return ReplayResult{}, fmt.Errorf("failed to read parquet WAL first offset: %w", err)
	}
	if firstOffset == 0 {
		return ReplayResult{}, nil
	}
	lastOffset, err := c.wal.LastOffset()
	if err != nil {
		return ReplayResult{}, fmt.Errorf("failed to read parquet WAL last offset: %w", err)
	}
	if lastOffset == 0 {
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

	err = c.wal.Replay(firstOffset, lastOffset, func(offset uint64, entry parquet.WALEntry) error {
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
			if err := c.applyReceiptFromReplay(blockNumber, input); err != nil {
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
	return result, nil
}

// applyReceiptFromReplay is the replay-time variant of applyReceipt: it
// rotates without writing to the WAL (the WAL is the source of replay) and
// drops temp-cache entries from the just-closed file's range.
func (c *Coordinator) applyReceiptFromReplay(blockNumber uint64, input parquet.ReceiptInput) error {
	if c.receiptWriter != nil && blockNumber != c.lastSeenBlock && c.isRotationBoundary(blockNumber) {
		if err := c.rotateOpenFileWithoutWAL(blockNumber); err != nil {
			return err
		}
		c.dropTempCacheBefore(c.fileStartBlock)
	}
	return c.applyReceipt(blockNumber, input)
}

// normalizeReplayInput backfills the ReceiptInput fields that the converter
// may have left empty (block number, tx hash, and the receipt byte
// payloads), so downstream apply code doesn't need replay-aware branches.
func normalizeReplayInput(blockNumber uint64, receiptBytes []byte, replayed ReplayReceipt) parquet.ReceiptInput {
	input := replayed.Input
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

// copyReceiptRecord returns a deep copy of record so callers can retain it
// without aliasing the converter's internal buffers.
func copyReceiptRecord(record parquet.ReceiptRecord) parquet.ReceiptRecord {
	return parquet.ReceiptRecord{
		TxHash:       append([]byte(nil), record.TxHash...),
		BlockNumber:  record.BlockNumber,
		ReceiptBytes: append([]byte(nil), record.ReceiptBytes...),
	}
}

// clearWALPreservingLast truncates the WAL up to (but not including) its
// last offset after a rotation. The final entry is retained so that crash
// recovery can still observe the rotation boundary.
func (c *Coordinator) clearWALPreservingLast() error {
	if c.wal == nil {
		return nil
	}
	firstOffset, err := c.wal.FirstOffset()
	if err != nil {
		return fmt.Errorf("failed to read parquet WAL first offset: %w", err)
	}
	if firstOffset == 0 {
		return nil
	}
	lastOffset, err := c.wal.LastOffset()
	if err != nil {
		return fmt.Errorf("failed to read parquet WAL last offset: %w", err)
	}
	if lastOffset == 0 {
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

// truncateReplayWAL drops WAL entries up to and including dropOffset after
// a successful replay. Out-of-range errors from the underlying WAL are
// treated as no-ops since they mean nothing was left to truncate.
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
