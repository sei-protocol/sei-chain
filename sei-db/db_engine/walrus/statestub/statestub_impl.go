package statestub

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/pebble"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// The subdirectory holding the live database.
const databaseDirName = "state"

// The subdirectory checkpoints are written into before they are handed to a caller.
const checkpointsDirName = "checkpoints"

var _ StateStub = (*stateStub)(nil)

// stateStub is a flat pebble store that can checkpoint itself.
//
// Writes run through a pipeline rather than on the caller's thread: a block is staged and sorted on one
// goroutine and applied to pebble on another, so the sort's CPU overlaps the write's I/O. The channels
// joining the stages are bounded, which is what makes the store falling behind show up as CommitBlock
// blocking rather than as memory filling.
type stateStub struct {
	config      *Config
	database    *pebble.DB
	checkpoints string

	// Blocks awaiting staging and sorting.
	staging chan stagedBlock

	// Sorted batches awaiting a write.
	writing chan sortedBlock

	// Blocks accepted but not yet written. Checkpoint and Close wait on it.
	pending sync.WaitGroup

	// How many blocks are in the pipeline, published so backpressure is visible before it bites.
	depth atomic.Int64

	// The last block written, which is what a checkpoint would contain.
	committed atomic.Uint64

	// The last block accepted, used to enforce contiguity. Only CommitBlock touches it.
	accepted uint64
	started  bool

	// Set when a write fails. A lost write means the checkpoints that follow are wrong, so the store stops
	// accepting work rather than producing a snapshot that disagrees with the log it is meant to match.
	failureLock sync.Mutex
	failure     error

	writerDone chan struct{}
	closeOnce  sync.Once
}

// stagedBlock is one block on its way into the pipeline.
type stagedBlock struct {
	blockNumber uint64
	changeSets  []*proto.NamedChangeSet
}

// sortedBlock is one block's changes, ordered by key and ready to apply.
type sortedBlock struct {
	blockNumber uint64
	changes     []stagedChange
}

// stagedChange is one key change waiting to be applied.
type stagedChange struct {
	key     []byte
	value   []byte
	deleted bool
}

// CommitBlock applies a block's changes.
//
// The write is scheduled rather than performed: this returns once the block is in the pipeline, and blocks
// only when the pipeline is full.
func (s *stateStub) CommitBlock(blockNumber uint64, changeSets []*proto.NamedChangeSet) error {
	if err := s.checkFailure(); err != nil {
		return err
	}
	if s.started && blockNumber != s.accepted+1 {
		return fmt.Errorf("block %d does not follow block %d", blockNumber, s.accepted)
	}
	for _, changeSet := range changeSets {
		if changeSet.Name != s.config.StoreName {
			return fmt.Errorf("block %d carries a changeset named %q, but this store holds %q",
				blockNumber, changeSet.Name, s.config.StoreName)
		}
	}

	s.accepted = blockNumber
	s.started = true
	s.pending.Add(1)
	s.depth.Add(1)
	s.staging <- stagedBlock{blockNumber: blockNumber, changeSets: changeSets}
	return nil
}

// Get returns the value key currently holds.
//
// It reflects what has been written, which lags what has been accepted while the pipeline drains.
func (s *stateStub) Get(key []byte) (value []byte, found bool, err error) {
	stored, closer, err := s.database.Get(key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to read the state stub: %w", err)
	}
	value = bytes.Clone(stored)
	if err := closer.Close(); err != nil {
		return nil, false, fmt.Errorf("failed to release a read of the state stub: %w", err)
	}
	return value, true, nil
}

// BlockNumber returns the last block written.
func (s *stateStub) BlockNumber() (ok bool, blockNumber uint64) {
	written := s.committed.Load()
	return s.started, written
}

// PendingBlocks returns how many blocks are in the pipeline.
func (s *stateStub) PendingBlocks() int {
	return int(s.depth.Load())
}

// Checkpoint writes an image of state as of the last written block into a fresh directory.
//
// The pipeline is drained first, so the checkpoint lands on a well defined block rather than on whatever
// happened to have been applied when the call arrived. The write buffer is then flushed to a table, because
// a checkpoint captures tables and the write ahead log, and this store keeps no log: without the flush,
// everything written since the last flush would be missing from the image, and a walk terminating on it
// would report recently written keys absent.
func (s *stateStub) Checkpoint() (directory string, blockNumber uint64, err error) {
	s.pending.Wait()
	if err := s.checkFailure(); err != nil {
		return "", 0, err
	}
	if !s.started {
		return "", 0, fmt.Errorf("nothing has been committed yet")
	}
	blockNumber = s.committed.Load()

	if err := s.database.Flush(); err != nil {
		return "", 0, fmt.Errorf("failed to flush before checkpointing at block %d: %w", blockNumber, err)
	}

	directory = filepath.Join(s.checkpoints, fmt.Sprintf("checkpoint-%020d", blockNumber))
	if err := os.RemoveAll(directory); err != nil {
		return "", 0, fmt.Errorf("failed to clear %s: %w", directory, err)
	}
	if err := s.database.Checkpoint(directory, pebble.WithFlushedWAL()); err != nil {
		return "", 0, fmt.Errorf("failed to checkpoint at block %d: %w", blockNumber, err)
	}
	return directory, blockNumber, nil
}

// Close drains the pipeline and releases the store's resources.
func (s *stateStub) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.staging)
		<-s.writerDone
		if closeErr := s.database.Close(); closeErr != nil {
			err = fmt.Errorf("failed to close the state stub: %w", closeErr)
		}
	})
	if err != nil {
		return err
	}
	return s.checkFailure()
}

// runSorter stages each block's changes into a flat slice and orders them by key.
//
// Sorting here rather than in the writer is what lets the ordering cost overlap the write it precedes.
func (s *stateStub) runSorter() {
	defer close(s.writing)

	for block := range s.staging {
		changes := make([]stagedChange, 0, countChanges(block.changeSets))
		for _, changeSet := range block.changeSets {
			for _, pair := range changeSet.Changeset.Pairs {
				changes = append(changes, stagedChange{
					key:     pair.Key,
					value:   pair.Value,
					deleted: pair.Delete,
				})
			}
		}

		// Applying in key order lets consecutive inserts reuse a warm search path through the write buffer,
		// where the scattered order a block arrives in walks it afresh every time. The sort is stable, so a
		// key written more than once in a block still ends with its last write.
		sort.SliceStable(changes, func(a int, b int) bool {
			return bytes.Compare(changes[a].key, changes[b].key) < 0
		})

		s.writing <- sortedBlock{blockNumber: block.blockNumber, changes: changes}
	}
}

// runWriter applies sorted batches to pebble.
func (s *stateStub) runWriter() {
	defer close(s.writerDone)

	for block := range s.writing {
		if err := s.write(block); err != nil {
			s.setFailure(err)
		}
		s.committed.Store(block.blockNumber)
		s.depth.Add(-1)
		// Every path has to reach this, including the failing one, or a drain would never finish.
		s.pending.Done()
	}
}

// write applies one sorted batch.
func (s *stateStub) write(block sortedBlock) error {
	batch := s.database.NewBatch()
	defer func() { _ = batch.Close() }()

	for _, change := range block.changes {
		var err error
		if change.deleted {
			err = batch.Delete(change.key, pebble.NoSync)
		} else {
			err = batch.Set(change.key, change.value, pebble.NoSync)
		}
		if err != nil {
			return fmt.Errorf("failed to stage a change from block %d: %w", block.blockNumber, err)
		}
	}
	if err := batch.Commit(pebble.NoSync); err != nil {
		return fmt.Errorf("failed to commit block %d: %w", block.blockNumber, err)
	}
	return nil
}

// setFailure latches the first failure, after which the store refuses further work.
func (s *stateStub) setFailure(err error) {
	s.failureLock.Lock()
	defer s.failureLock.Unlock()
	if s.failure == nil {
		s.failure = err
	}
}

// checkFailure reports the latched failure, if any.
func (s *stateStub) checkFailure() error {
	s.failureLock.Lock()
	defer s.failureLock.Unlock()
	return s.failure
}

// countChanges reports how many key changes a block's changesets hold.
func countChanges(changeSets []*proto.NamedChangeSet) int {
	count := 0
	for _, changeSet := range changeSets {
		count += len(changeSet.Changeset.Pairs)
	}
	return count
}

// New opens a StateStub in the configured directory.
func New(config *Config) (StateStub, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	checkpoints := filepath.Join(config.Path, checkpointsDirName)
	if err := os.MkdirAll(checkpoints, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", checkpoints, err)
	}

	database, err := openDatabase(config)
	if err != nil {
		return nil, err
	}

	created := &stateStub{
		config:      config,
		database:    database,
		checkpoints: checkpoints,
		staging:     make(chan stagedBlock, config.PipelineDepth),
		writing:     make(chan sortedBlock, config.PipelineDepth),
		writerDone:  make(chan struct{}),
	}
	go created.runSorter()
	go created.runWriter()
	return created, nil
}

// openDatabase opens the pebble database backing the store.
//
// The options are set here rather than taken from the shared pebbledb wrapper because this store is a
// benchmark stand-in with different needs: it wants compaction concurrency above the underlying default of
// one, a write buffer sized for a sustained ingest rate, and no write ahead log.
func openDatabase(config *Config) (*pebble.DB, error) {
	cache := pebble.NewCache(config.CacheSize)
	defer cache.Unref()

	options := &pebble.Options{
		Cache:                    cache,
		DisableWAL:               !config.KeepWriteAheadLog,
		MemTableSize:             config.MemTableSize,
		MaxConcurrentCompactions: func() int { return config.CompactionConcurrency },
		Logger:                   silentLogger{},
	}
	options.EnsureDefaults()

	path := filepath.Join(config.Path, databaseDirName)
	database, err := pebble.Open(path, options)
	if err != nil {
		return nil, fmt.Errorf("failed to open the state stub database at %s: %w", path, err)
	}
	return database, nil
}

// silentLogger discards what the database has to say, which is startup and compaction narration that tells
// an operator nothing about the engine under measurement.
type silentLogger struct{}

// Infof discards the message.
func (silentLogger) Infof(_ string, _ ...any) {}

// Fatalf discards the message. Pebble only reaches this on a corrupt database, which surfaces as an error
// from the operation that provoked it.
func (silentLogger) Fatalf(_ string, _ ...any) {}
