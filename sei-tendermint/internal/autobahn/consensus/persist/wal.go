package persist

import (
	"fmt"
	"os"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

// targetFileSize is the size a WAL file may reach before it is sealed and a fresh one is opened.
//
// File size is the granularity at which space is reclaimed: a sealed file is deleted only once every
// record in it is prunable, never partially, and the file being written is never deleted at all. Raising
// this trades later reclamation for fewer files to track. A large value costs autobahn little because
// these records are consumed by new proposals and pruned shortly after, so few accumulate either way.
const targetFileSize = 1 * unit.GB

// codec is the marshal/unmarshal pair needed to store T in a WAL.
// protoutils.Conv[T, P] satisfies this interface automatically.
type codec[T any] interface {
	Marshal(T) []byte
	Unmarshal([]byte) (T, error)
}

// walEntry is one record read back from a WAL.
type walEntry[T any] struct {
	// The index the record is stored under: a block number in a lane WAL, a road index in the
	// commitQC WAL.
	index uint64
	// The decoded record.
	value T
}

// openWAL creates (or opens) a WAL in dir, recovering any files left behind by a previous session.
//
// name identifies the WAL in its metrics and has no effect on the on-disk layout.
//
// metricsEnabled must be false when other WALs live under the same name at the same time. Their counters
// would aggregate, but seiwal's queue-depth gauge is keyed on the name alone, so concurrent instances
// sharing one overwrite each other's samples rather than summing them.
//
// fileSize is the size a file may reach before it is sealed, which is also the granularity at which
// pruning reclaims space. Production callers pass targetFileSize.
//
// Appends to the returned WAL are only durable once Flush returns; a caller that reports a record as
// persisted must flush first.
func openWAL[T any](
	dir string,
	name string,
	codec codec[T],
	fileSize uint,
	metricsEnabled bool,
) (seiwal.WAL[T], error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create dir %s: %w", dir, err)
	}

	config := seiwal.DefaultConfig(dir, name)
	// Consensus safety rests on a persisted entry surviving power loss, not merely a process crash.
	config.FsyncOnFlush = true
	// When the prune anchor moves past every record a WAL holds, the next append lands above the
	// highest index on disk rather than one past it. Contiguity within the live range is still
	// enforced by the callers of Append, which reject an out-of-sequence block or road index.
	config.PermitGaps = true
	config.TargetFileSize = fileSize
	config.MetricsEnabled = metricsEnabled
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid WAL config for %s: %w", dir, err)
	}

	wal, err := seiwal.NewGenericWAL[T](
		config,
		func(entry T) ([]byte, error) { return codec.Marshal(entry), nil },
		codec.Unmarshal,
	)
	if err != nil {
		return nil, fmt.Errorf("open WAL in %s: %w", dir, err)
	}
	return wal, nil
}

// readAll returns every record the WAL currently holds in ascending index order, or nil when it holds
// none.
//
// Pruning is lazy, so the result may include records below the last requested prune point. Callers
// decide what to do with those; see contiguousSuffix.
func readAll[T any](w seiwal.WAL[T]) (entries []walEntry[T], err error) {
	ok, first, last, err := w.Bounds()
	if err != nil {
		return nil, fmt.Errorf("read WAL bounds: %w", err)
	}
	if !ok {
		return nil, nil
	}

	it, err := w.Iterator(first, last)
	if err != nil {
		return nil, fmt.Errorf("open WAL iterator over [%d, %d]: %w", first, last, err)
	}
	defer func() {
		if closeErr := it.Close(); closeErr != nil && err == nil {
			entries, err = nil, fmt.Errorf("close WAL iterator: %w", closeErr)
		}
	}()

	for {
		more, nextErr := it.Next()
		if nextErr != nil {
			return nil, fmt.Errorf("advance WAL iterator: %w", nextErr)
		}
		if !more {
			return entries, nil
		}
		index, value := it.Entry()
		entries = append(entries, walEntry[T]{index: index, value: value})
	}
}

// contiguousSuffix returns the longest run of entries that ends at the final entry and whose indices
// each exceed their predecessor by exactly one.
//
// A gap marks the boundary between garbage and live data. Pruning only deletes whole sealed files, so
// records the prune anchor has moved past survive until every record in their file is below the
// threshold; appending at the anchor's index then leaves a hole behind it. Everything before the last
// hole was pruned and merely not yet reclaimed.
//
// Dropping that prefix is not a corruption check — a hole left by genuinely lost data is discarded here
// just the same. What catches real loss is avail.newInner, which rejects loaded records that do not
// begin exactly where the prune anchor says they should, so a lost sealed file fails startup instead of
// resuming from a truncated history. This discard is only safe while that holds.
func contiguousSuffix[T any](entries []walEntry[T]) []walEntry[T] {
	for i := len(entries) - 1; i > 0; i-- {
		if entries[i].index != entries[i-1].index+1 {
			return entries[i:]
		}
	}
	return entries
}
