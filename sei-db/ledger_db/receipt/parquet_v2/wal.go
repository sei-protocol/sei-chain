package parquet_v2

func (c *coordinator) replayWAL(converter WALReceiptConverter) (ReplayResult, error) {
	_ = c
	_ = converter
	return ReplayResult{}, ErrNotImplemented
}

func truncateReplayWAL(w interface{ TruncateBefore(offset uint64) error }, dropOffset uint64) error {
	_ = w
	_ = dropOffset
	return ErrNotImplemented
}
