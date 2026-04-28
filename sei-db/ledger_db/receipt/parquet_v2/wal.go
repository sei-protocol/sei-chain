package parquet_v2

import (
	"fmt"
	"strings"
)

func (c *coordinator) replayWAL(converter WALReceiptConverter) (ReplayResult, error) {
	_ = c
	_ = converter
	return ReplayResult{}, ErrNotImplemented
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
	_ = w
	_ = dropOffset
	return ErrNotImplemented
}
