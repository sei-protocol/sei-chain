package wrappers

import (
	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// This benchmarking utility is capable of benchmarking a DB that implements this interface.
type DBWrapper interface {
	// ApplyChangeSets applies a versioned changelog entry. SC-backed wrappers buffer
	// entry changesets until Commit, while SS-backed wrappers can use entry.Version
	// to persist at the benchmark-assigned version immediately.
	ApplyChangeSets(entry *proto.ChangelogEntry) error

	// Read reads the value for the given key.
	Read(key []byte) (data []byte, found bool, err error)

	// Commit persists buffered writes and advances the version.
	Commit() (int64, error)

	// Close releases any resources held by the DB.
	Close() error

	// Version returns the latest committed version.
	Version() int64

	// LoadLatest opens the DB at its latest committed version. Benchmarks only ever run against the tip, so
	// there is no way to ask for a historical version.
	LoadLatest() error

	// Importer return an importer which load snapshot data into the database
	Importer(version int64) (types.Importer, error)

	// AwaitBlockHash blocks until the DB has produced the hash of the given version. A DB that does not hash
	// blocks in the background returns immediately.
	//
	// A DB that does hash in the background publishes each block's hash on a stream, and a stream nobody reads
	// eventually stalls commits, so a benchmark run has to consume it. This is that consumer, and how far the
	// version asked for trails the version committed is how much asynchrony the run allows the hasher.
	AwaitBlockHash(version int64) error

	// Get the phase timer used to measure time spent in various phases of execution. Useful for metrics
	// integration with external phases of execution.
	//
	// If the underlying DB does not support phase timers, return nil.
	GetPhaseTimer() *metrics.PhaseTimer
}
