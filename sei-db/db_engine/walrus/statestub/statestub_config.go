package statestub

import (
	"fmt"
	"regexp"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
)

// The permitted shape of an instance name: it becomes a metric attribute value, so it is restricted to
// characters safe for label values.
var nameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Config configures a StateStub.
type Config struct {
	// The directory where the store keeps its data and stages checkpoints.
	Path string

	// A short identifier for this instance, used to distinguish its metrics from those of other instances in
	// the same process. Required; must match [a-zA-Z0-9_-]+.
	Name string

	// The name of the store whose changesets this instance applies. CommitBlock rejects a changeset carrying
	// any other name.
	StoreName string

	// The size the write buffer may reach before it is flushed to a table.
	//
	// Several of these are resident at once, one filling and the rest awaiting flush, so the memory this
	// costs is a multiple of what it says.
	MemTableSize uint64

	// The block cache size. Nothing reads this store during a run except compaction, so a large cache buys
	// little.
	CacheSize int64

	// How many compactions may run at once. The underlying default is one, which cannot keep up with a
	// sustained write rate: tables pile up in the first level and everything downstream degrades.
	CompactionConcurrency int

	// Whether to keep a write ahead log.
	//
	// The default is not to. This store exists to produce checkpoints for a benchmark, and the benchmark
	// cannot resume a run in any case, so a crash replays from the first block whether or not the log was
	// kept. Keeping it writes a second copy of every byte for nothing.
	KeepWriteAheadLog bool

	// How many blocks may be in the sort and write pipeline before CommitBlock blocks.
	//
	// Blocking is the point: it is what turns the store falling behind into visible backpressure rather than
	// into unbounded memory growth.
	PipelineDepth int
}

// DefaultConfig returns a default configuration for the store at path, identified by name, holding the store
// named storeName.
func DefaultConfig(path string, name string, storeName string) *Config {
	return &Config{
		Path:                  path,
		Name:                  name,
		StoreName:             storeName,
		MemTableSize:          256 * unit.MB,
		CacheSize:             128 * unit.MB,
		CompactionConcurrency: 4,
		KeepWriteAheadLog:     false,
		PipelineDepth:         4,
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
	if c.StoreName == "" {
		return fmt.Errorf("store name is required")
	}
	if c.MemTableSize == 0 {
		return fmt.Errorf("mem table size must be greater than 0")
	}
	if c.CacheSize <= 0 {
		return fmt.Errorf("cache size must be greater than 0")
	}
	if c.CompactionConcurrency <= 0 {
		return fmt.Errorf("compaction concurrency must be greater than 0")
	}
	if c.PipelineDepth <= 0 {
		return fmt.Errorf("pipeline depth must be greater than 0")
	}
	return nil
}
