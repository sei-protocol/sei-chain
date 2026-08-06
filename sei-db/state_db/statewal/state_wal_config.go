package statewal

import (
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/management/gc"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

// Configuration for a state WAL.
type Config struct {
	// The directory where the WAL writes its files.
	Path string

	// A short identifier for this WAL instance, used to distinguish its metrics from those of other
	// instances in the same process. Required; must match [a-zA-Z0-9_-]+.
	Name string

	// The size of the channel used to send work from the caller to the serialization goroutine.
	RequestBufferSize uint

	// The size of the channel used to send framed records from the underlying WAL's serialization to its
	// writer goroutine.
	WriteBufferSize uint

	// The size a WAL file may reach before it is sealed and a fresh one is opened. Because each block is
	// written as a single record, a file may exceed this by the size of one block's serialized changesets.
	// Must be greater than 0.
	TargetFileSize uint

	// When true, Flush calls fsync on the underlying file so that flushed data survives a power loss, not
	// merely a process crash. When false, Flush only flushes the in-process buffer to the OS.
	FsyncOnFlush bool

	// The number of blocks an iterator's reader thread may prefetch ahead of the consumer. A larger value
	// keeps the reader busy while the consumer processes blocks, which matters for startup replay speed.
	// Must be greater than 0.
	IteratorPrefetchSize uint

	// The interval at which the underlying WAL samples the buffered depth of its internal channels into the
	// seiwal_queue_depth gauge. Zero or negative disables sampling.
	MetricsSampleInterval time.Duration

	// RetentionWindow is how much history this WAL keeps beyond the shared rollback window of the
	// StorageGarbageCollector that manages it, in blocks. It is what gc.PrunableStore.GetRetentionWindow
	// answers:
	//
	//	> 0  → that many blocks of history beyond the rollback window
	//	0    → the rollback window only, plus whatever the other managed stores hold it back for
	//	-1   → never prune this WAL (gc.InfiniteRetentionWindow)
	//
	// Zero does NOT mean "keep everything" here, unlike the KeepRecent fields on StateStoreConfig and
	// ReceiptStoreConfig, where 0 disables pruning. It is the most aggressive setting this field has;
	// "keep everything" is -1. Assigning a KeepRecent value to this field inverts the retention it
	// asks for.
	//
	// Leave this at 0 unless something outside SC/SS needs the history. The WAL is what SC and SS replay
	// from, and they already hold it back on their own: each answers its oldest live snapshot as its
	// pruning boundary, and the collector prunes every store to the shared minimum. Depth declared here is
	// additive on top of that, and — because the minimum is shared — it retains the whole fleet that much
	// further back, not just this WAL. Its purpose is a need the snapshot stores do not express, such as
	// serving catch-up to a peer further behind than any live snapshot.
	//
	// -1 is for a WAL that must never be reclaimed at all. It is not a way to protect a replay range: a
	// store with no cut line is never asked for a boundary and never pruned, so the WAL simply grows
	// without bound. Must be >= gc.InfiniteRetentionWindow.
	RetentionWindow int64
}

// Constructor for a default state WAL configuration for the WAL at path, identified by name.
//
// RetentionWindow defaults to 0 — no history beyond what SC and SS hold the WAL back for — because that is
// the depth the WAL is actually required to have, and any other default would retain every managed store
// that much further back. See the field for when to raise it.
func DefaultConfig(path string, name string) *Config {
	s := seiwal.DefaultConfig(path, name)
	return &Config{
		Path:                  path,
		Name:                  name,
		RequestBufferSize:     16,
		WriteBufferSize:       s.WriteBufferSize,
		TargetFileSize:        s.TargetFileSize,
		FsyncOnFlush:          s.FsyncOnFlush,
		IteratorPrefetchSize:  s.IteratorPrefetchSize,
		MetricsSampleInterval: s.MetricsSampleInterval,
		RetentionWindow:       0,
	}
}

// Validate the configuration, returning nil if valid, or an error describing the problem if invalid.
func (c *Config) Validate() error {
	if c.RetentionWindow < gc.InfiniteRetentionWindow {
		return fmt.Errorf("RetentionWindow must be >= %d (got %d)",
			gc.InfiniteRetentionWindow, c.RetentionWindow)
	}
	return c.toSeiwalConfig().Validate()
}

// toSeiwalConfig maps this configuration onto the underlying generic WAL's configuration.
func (c *Config) toSeiwalConfig() *seiwal.Config {
	return &seiwal.Config{
		Path:                 c.Path,
		Name:                 c.Name,
		WriteBufferSize:      c.WriteBufferSize,
		SerializerBufferSize: c.RequestBufferSize,
		TargetFileSize:       c.TargetFileSize,
		FsyncOnFlush:         c.FsyncOnFlush,
		// State blocks must be contiguous — no skipped blocks — so the underlying WAL rejects gaps.
		PermitGaps:            false,
		IteratorPrefetchSize:  c.IteratorPrefetchSize,
		MetricsSampleInterval: c.MetricsSampleInterval,
	}
}
