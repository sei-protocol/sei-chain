package datalake

// putWrite writes a value held in memory at a key.
type putWrite struct {
	// The key written.
	key uint64

	// The value written.
	value []byte

	// Receives the outcome.
	result *writeResult
}

// streamPutWrite writes the bytes read from a stream at a key.
type streamPutWrite struct {
	// The key written.
	key uint64

	// The value written. Closed when the write finishes.
	stream DataLakeStream

	// Receives the outcome.
	result *writeResult
}

// deleteWrite deletes a key.
type deleteWrite struct {
	// The key deleted.
	key uint64

	// Receives the outcome.
	result *writeResult
}

// metadataWrite replaces the content of the store's metadata object.
type metadataWrite struct {
	// The new content of the metadata object.
	data []byte

	// Receives the outcome.
	result *writeResult
}

// writeResult is the outcome of a write or delete submitted to a bucketWriter. Its err is valid once done is closed.
type writeResult struct {
	// The key written or deleted, or zero for a metadata write.
	key uint64

	// The write's failure, if any.
	err error

	// Closed when the write finishes.
	done chan struct{}
}

// newWriteResult returns the outcome of a write of key, not yet complete.
func newWriteResult(key uint64) *writeResult {
	return &writeResult{key: key, done: make(chan struct{})}
}

// complete records the outcome of the write and wakes anyone waiting for it. Only the bucket writer calls it,
// exactly once.
func (result *writeResult) complete(err error) {
	result.err = err
	close(result.done)
}

// Key returns the key written or deleted, or zero for a metadata write.
func (result *writeResult) Key() uint64 {
	return result.key
}

// Wait blocks until the write finishes and returns its failure, if any.
func (result *writeResult) Wait() error {
	<-result.done
	return result.err
}

// Check returns the write's failure, if any, without blocking.
func (result *writeResult) Check() (
	err error,
	// False if the write has not finished, in which case err is nil.
	done bool,
) {
	select {
	case <-result.done:
		return result.err, true
	default:
		return nil, false
	}
}
