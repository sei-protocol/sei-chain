package cdkdatalake

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	"gocloud.dev/blob/fileblob"
)

// DataLakeConfig configures a datalake.DataLake opened by NewCDKDataLake().
type DataLakeConfig struct {
	// The Go CDK URL of the bucket holding the store's objects, such as s3://my-bucket. Several stores may share a
	// bucket, each keeping its objects under its own name.
	BucketURL string

	// The minimum time between the starts of metadata writes made while the store's watermarks have changed. The
	// metadata writes that Flush() and a store's first put require do not wait for it.
	MetadataWriteInterval time.Duration

	// The number of goroutines writing and deleting objects.
	WriterCount int

	// The number of goroutines reading objects.
	ReaderCount int

	// The maximum number of writes and deletes waiting for a writer goroutine. Callers block while it is reached.
	WorkBufferSize int

	// The maximum number of requests waiting for the control loop. Callers block while it is reached.
	RequestBufferSize int

	// The maximum number of deletes the control loop hands to the writer each time it wakes. Must be greater than
	// 1.
	DeletesPerCycle int

	// How long to wait before retrying a backing store operation that failed with a transient error.
	RetryBackoff time.Duration

	// If true, an operation that fails with a transient error is retried until it succeeds.
	InfiniteRetries bool

	// The number of times an operation may be retried before the store fails, when InfiniteRetries is false.
	MaxRetries int

	// The number of values a prefetching iterator fetches ahead of its position.
	PrefetchDepth int
}

// DefaultDataLakeConfig returns the default configuration for a datalake.DataLake opened by NewCDKDataLake().
func DefaultDataLakeConfig() DataLakeConfig {
	return DataLakeConfig{
		MetadataWriteInterval: time.Second,
		WriterCount:           16,
		ReaderCount:           16,
		WorkBufferSize:        1024,
		RequestBufferSize:     1024,
		DeletesPerCycle:       8,
		RetryBackoff:          time.Second,
		InfiniteRetries:       true,
		MaxRetries:            4,
		PrefetchDepth:         8,
	}
}

// DefaultTestDataLakeConfig returns a configuration for tests that keeps the store's objects in the directory dir.
func DefaultTestDataLakeConfig(dir string) DataLakeConfig {
	config := DefaultDataLakeConfig()
	config.BucketURL = (&url.URL{Scheme: fileblob.Scheme, Path: filepath.ToSlash(dir)}).String()
	config.MetadataWriteInterval = 5 * time.Millisecond
	config.WriterCount = 4
	config.ReaderCount = 4
	config.WorkBufferSize = 64
	config.RequestBufferSize = 64
	config.DeletesPerCycle = 2
	config.RetryBackoff = time.Millisecond
	config.PrefetchDepth = 3
	return config
}

// Validate returns an error if the configuration is unusable.
func (c DataLakeConfig) Validate() error {
	if c.BucketURL == "" {
		return errors.New("bucket URL must be set")
	}
	if c.MetadataWriteInterval <= 0 {
		return fmt.Errorf("metadata write interval must be positive, got %s", c.MetadataWriteInterval)
	}
	if c.WriterCount <= 0 {
		return fmt.Errorf("writer count must be positive, got %d", c.WriterCount)
	}
	if c.ReaderCount <= 0 {
		return fmt.Errorf("reader count must be positive, got %d", c.ReaderCount)
	}
	if c.WorkBufferSize <= 0 {
		return fmt.Errorf("work buffer size must be positive, got %d", c.WorkBufferSize)
	}
	if c.RequestBufferSize <= 0 {
		return fmt.Errorf("request buffer size must be positive, got %d", c.RequestBufferSize)
	}
	if c.DeletesPerCycle <= 1 {
		return fmt.Errorf("deletes per cycle must be greater than 1, got %d", c.DeletesPerCycle)
	}
	if c.RetryBackoff < 0 {
		return fmt.Errorf("retry backoff must not be negative, got %s", c.RetryBackoff)
	}
	if c.MaxRetries < 0 {
		return fmt.Errorf("max retries must not be negative, got %d", c.MaxRetries)
	}
	if c.PrefetchDepth <= 0 {
		return fmt.Errorf("prefetch depth must be positive, got %d", c.PrefetchDepth)
	}
	return nil
}

// mayRetry reports whether an operation already retried retries times may be retried again.
func (c DataLakeConfig) mayRetry(retries int) bool {
	return c.InfiniteRetries || retries < c.MaxRetries
}
