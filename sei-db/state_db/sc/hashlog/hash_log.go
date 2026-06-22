package hashlog

// A record for a single block, recording information about the block's hash.
type HashLog struct {
	// The software version currently running on this node.
	Version string

	// The block number for this log entry.
	BlockNumber uint64

	// The hash of the state diff for this block. Nil if no hash was recorded or if the diff wasn't hashed due
	// to load shedding.
	DiffHash []byte

	// The hash of the FlatKV state after processing this block. Nil if FlatKV is not enabled.
	FlatKVHash []byte

	// The hash of the MemIAVL state after processing this block. Nil if MemIAVL is not enabled.
	MemIAVLHash []byte

	// The root hash of the block, which should be the hash of the block header.
	RootHash []byte
}

// Get the CSV header for a HashLog.
func hashLogHeaders() string {
	return "" // TODO
}

// Convert a HashLog to a CSV string using "," as the separator.
func (h *HashLog) toCSV() string {
	return "" // TODO
}

// Parse a HashLog from a CSV string. The CSV should have the same format as produced by toCSV.
func hashLogFromCSV(csv string) (*HashLog, error) {
	return nil, nil // TODO
}
