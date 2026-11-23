package datalake

import "context"

// DataLakeIterator visits the keys of a DataLake in key order. It is not safe for concurrent use.
type DataLakeIterator interface {
	// Next advances to the next key. It returns false once no keys remain.
	Next(ctx context.Context) (bool, error)

	// Key returns the current key. It may only be called after Next() returns true.
	Key() uint64

	// Value returns the value at the current key. It may only be called after Next() returns true. It may return an
	// error if the key was deleted after the iterator was created.
	Value(ctx context.Context) ([]byte, error)

	// Close releases the iterator's resources, including any values fetched ahead.
	Close() error
}
