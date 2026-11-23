package hashlog

// Asks the writer goroutine to append one block's hashes as one row.
type writeBlockRequest struct {

	// The block number.
	blockNumber uint64

	// The block's hashes, keyed by name.
	hashes map[string][]byte
}

// Asks the writer goroutine to flush and fsync everything written so far.
type flushRequest struct {

	// Receives the outcome of the flush.
	reply chan error
}

// Asks the writer goroutine to seal the mutable file and stop.
type closeRequest struct{}
