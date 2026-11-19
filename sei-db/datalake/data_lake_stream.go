package datalake

import "io"

var _ io.Reader = DataLakeStream(nil)

// DataLakeStream is a value read as a stream that can be restarted from its first byte.
type DataLakeStream interface {
	// Read reads the next bytes of the stream into p, with the semantics of io.Reader.
	Read(p []byte) (n int, err error)

	// Reset restarts the stream from its first byte.
	Reset() error

	// Close releases the stream's resources. No other method is called afterwards.
	Close() error
}
