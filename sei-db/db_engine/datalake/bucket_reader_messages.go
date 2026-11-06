package datalake

import (
	"context"
	"fmt"
	"io"
)

// getRead reads a key's value.
type getRead struct {
	// The key read.
	key uint64

	// Receives the outcome.
	result *getResult
}

// getResult is the outcome of a Get(). Its fields other than key are valid once done is closed.
type getResult struct {
	// The key read.
	key uint64

	// The value read.
	value []byte

	// False if the key's object does not exist.
	found bool

	// The read's failure, if any.
	err error

	// Closed when the read finishes.
	done chan struct{}
}

// newGetResult returns the outcome of a Get() of key, not yet complete.
func newGetResult(key uint64) *getResult {
	return &getResult{key: key, done: make(chan struct{})}
}

// complete records the outcome of the read and wakes anyone waiting for it. Only the bucket reader calls it, exactly
// once.
func (result *getResult) complete(
	value []byte,
	// False if the key's object does not exist.
	found bool,
	err error,
) {
	result.value = value
	result.found = found
	result.err = err
	close(result.done)
}

// Key returns the key read.
func (result *getResult) Key() uint64 {
	return result.key
}

// Value returns the value read. It returns an error if the read failed or ctx ends first.
func (result *getResult) Value(
	ctx context.Context,
) (
	value []byte,
	// False if the key's object does not exist.
	found bool,
	err error,
) {
	if err := waitForRead(ctx, result.done); err != nil {
		return nil, false, fmt.Errorf("read of key %d: %w", result.key, err)
	}
	return result.value, result.found, result.err
}

// streamRead opens a reader over a key's value. Its fields other than key are valid once done is closed.
type streamRead struct {
	// The key read.
	key uint64

	// The reader opened.
	stream io.ReadCloser

	// False if the key's object does not exist.
	found bool

	// The read's failure, if any.
	err error

	// Closed when the read finishes.
	done chan struct{}
}

// hasRead checks whether a key's object exists. Its fields other than key are valid once done is closed.
type hasRead struct {
	// The key checked.
	key uint64

	// True if the key's object exists.
	found bool

	// The read's failure, if any.
	err error

	// Closed when the read finishes.
	done chan struct{}
}

// metadataRead reads the store's metadata object. Its fields are valid once done is closed.
type metadataRead struct {
	// The content of the metadata object.
	data []byte

	// False if the metadata object does not exist.
	found bool

	// The read's failure, if any.
	err error

	// Closed when the read finishes.
	done chan struct{}
}

// keyPageRead lists one page of the store's data objects. Its fields other than pageToken are valid once done is
// closed.
type keyPageRead struct {
	// The token naming the page.
	pageToken []byte

	// The names of the objects listed.
	names []string

	// The token naming the next page, or empty if this page was the last.
	nextPageToken []byte

	// The read's failure, if any.
	err error

	// Closed when the read finishes.
	done chan struct{}
}

// waitForRead blocks until done is closed or ctx ends. It returns an error if ctx ends first.
func waitForRead(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop waiting for the read: %w", ctx.Err())
	}
}
