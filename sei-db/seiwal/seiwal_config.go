package seiwal

import (
	"fmt"
	"regexp"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
)

// The permitted shape of a WAL instance name: it becomes a metric attribute value, so it is restricted to
// characters safe for label values.
var nameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Config configures a WAL.
type Config struct {
	// The directory where the WAL writes its files.
	Path string

	// A short identifier for this WAL instance, used to distinguish its metrics from those of other
	// instances in the same process. Required; must match [a-zA-Z0-9_-]+.
	Name string

	// The size of the channel used to send framed records and control messages to the writer goroutine.
	WriteBufferSize uint

	// The depth of the serialization request queue. Used only by the generic serializing WAL
	// (NewGenericWAL); the byte-oriented engine ignores it.
	SerializerBufferSize uint

	// The size a WAL file may reach before it is sealed and a fresh one is opened. Rotation happens after a
	// record is appended, so a file may exceed this by the size of a single record — and because a record
	// is written atomically to a single file, a record larger than this threshold produces a file that
	// overshoots it by that record's size. Must be greater than 0.
	TargetFileSize uint

	// When true, Flush calls fsync on the underlying file so that flushed data survives a power loss, not
	// merely a process crash. When false, Flush only flushes the in-process buffer to the OS.
	FsyncOnFlush bool

	// The number of records an iterator's reader thread may prefetch ahead of the consumer. A larger value
	// keeps the reader busy while the consumer processes records, which matters for startup replay speed.
	// Must be greater than 0.
	IteratorPrefetchSize uint

	// The interval at which the WAL samples the buffered depth of its internal channel into the
	// seiwal_queue_depth gauge. Zero or negative disables sampling.
	MetricsSampleInterval time.Duration
}

// DefaultConfig returns a default WAL configuration for the WAL at path, identified by name.
func DefaultConfig(path string, name string) *Config {
	return &Config{
		Path:                  path,
		Name:                  name,
		WriteBufferSize:       16,
		SerializerBufferSize:  16,
		TargetFileSize:        64 * unit.MB,
		FsyncOnFlush:          true,
		IteratorPrefetchSize:  32,
		MetricsSampleInterval: 15 * time.Second,
	}
}

// Validate the configuration, returning nil if valid, or an error describing the problem if invalid.
func (c *Config) Validate() error {
	if c.Path == "" {
		return fmt.Errorf("path is required")
	}
	if !nameRegex.MatchString(c.Name) {
		return fmt.Errorf("name %q is required and must match %s", c.Name, nameRegex.String())
	}
	if c.TargetFileSize == 0 {
		// A zero target would seal and rotate a fresh file after every single record.
		return fmt.Errorf("target file size must be greater than 0")
	}
	if c.IteratorPrefetchSize == 0 {
		return fmt.Errorf("iterator prefetch size must be greater than 0")
	}
	return nil
}
