package wal

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var _ FlatKVWalIterator = (*walIterator)(nil)

// A block produced by the reader goroutine, or a terminal error.
type iteratorResult struct {
	entry *FlatKVWalEntry
	err   error
}

// walIterator iterates the WAL a block at a time, in ascending block order. A dedicated reader goroutine reads
// WAL files from disk, coalesces each block's records (one per Write call, plus its end-of-block marker) into a
// single entry, and pushes it onto a buffered channel; Next simply dequeues. Decoupling disk reads from the
// consumer keeps the reader busy while the consumer works, which matters for startup replay speed. The reader
// loads one file at a time, so its memory use is bounded by a single WAL file plus the prefetch buffer.
//
// A read lease (pinnedBlock) holds the files the reader needs against concurrent pruning; Close releases it.
type walIterator struct {
	// The WAL this iterator reads from.
	wal *flatKVWalImpl

	// The lowest block the consumer asked for; blocks below it are skipped.
	start uint64

	// The block pinned as this iterator's read lease, released on Close.
	pinnedBlock uint64

	// Coalesced blocks produced by the reader goroutine. Closed by the reader on clean EOF.
	results chan iteratorResult

	// Closed by Close to tell the reader goroutine to stop early.
	stop chan struct{}

	// Closed by the reader goroutine when it exits, so Close can wait for it.
	readerExited chan struct{}

	// Ensures the shutdown sequence runs at most once.
	closeOnce sync.Once

	// The entry returned by Entry, set by the most recent successful Next. Consumer-owned.
	result *FlatKVWalEntry

	// Set once iteration is complete. Consumer-owned.
	done bool

	// The following fields are owned exclusively by the reader goroutine.

	// The smallest file index not yet consumed.
	nextIndex uint64
	// The records loaded from the current file, filtered to complete blocks at or beyond start.
	buffer []*FlatKVWalEntry
	// The position within buffer; -1 before the first record is read.
	pos int
}

// newWalIterator creates an iterator over wal starting at startingBlockNumber and launches its reader
// goroutine. pinnedBlock is the read lease registered on the iterator's behalf, released by Close.
// prefetch is the number of blocks the reader may buffer ahead of the consumer.
func newWalIterator(wal *flatKVWalImpl, startingBlockNumber uint64, pinnedBlock uint64, prefetch uint) *walIterator {
	it := &walIterator{
		wal:          wal,
		start:        startingBlockNumber,
		pinnedBlock:  pinnedBlock,
		results:      make(chan iteratorResult, prefetch),
		stop:         make(chan struct{}),
		readerExited: make(chan struct{}),
		pos:          -1,
	}
	go it.read()
	return it
}

func (it *walIterator) Next() (bool, error) {
	if it.done {
		return false, nil
	}
	result, ok := <-it.results
	if !ok {
		it.done = true
		return false, nil
	}
	if result.err != nil {
		it.done = true
		return false, result.err
	}
	it.result = result.entry
	return true, nil
}

func (it *walIterator) Entry() *FlatKVWalEntry {
	return it.result
}

func (it *walIterator) Close() error {
	it.closeOnce.Do(func() {
		close(it.stop)    // tell the reader to stop if it is mid-read
		<-it.readerExited // wait for it to actually exit before releasing resources
		it.wal.unpinBlock(it.pinnedBlock)
	})
	it.done = true
	return nil
}

// read is the reader goroutine: it produces coalesced blocks onto the results channel until the WAL is
// exhausted (then closes the channel), a read fails (then sends the error), or Close signals a stop.
func (it *walIterator) read() {
	defer close(it.readerExited)
	for {
		block, ok, err := it.nextBlock()
		if err != nil {
			it.send(iteratorResult{err: err})
			return
		}
		if !ok {
			close(it.results)
			return
		}
		if !it.send(iteratorResult{entry: block}) {
			return // Close signalled a stop
		}
	}
}

// send pushes a result onto the channel, returning false if Close signalled a stop first.
func (it *walIterator) send(result iteratorResult) bool {
	select {
	case it.results <- result:
		return true
	case <-it.stop:
		return false
	}
}

// nextBlock assembles the next block by draining records until it consumes that block's end-of-block marker.
// Returns ok=false once no records remain.
func (it *walIterator) nextBlock() (*FlatKVWalEntry, bool, error) {
	var block *FlatKVWalEntry
	for {
		record, ok, err := it.nextRecord()
		if err != nil {
			return nil, false, err
		}
		if !ok {
			// End of stream. A complete block always ends with an end-of-block marker, so reaching here
			// mid-block should not happen; emit any assembled changes defensively rather than dropping them.
			if block != nil {
				return block, true, nil
			}
			return nil, false, nil
		}
		if block == nil {
			block = &FlatKVWalEntry{BlockNumber: record.BlockNumber}
		}
		if record.EndOfBlock {
			return block, true, nil
		}
		block.Changeset = append(block.Changeset, record.Changeset...)
	}
}

// nextRecord returns the next individual record (changeset or end-of-block marker) in ascending order,
// advancing across files as needed. It returns ok=false once no further records remain.
func (it *walIterator) nextRecord() (*FlatKVWalEntry, bool, error) {
	for {
		it.pos++
		if it.pos < len(it.buffer) {
			return it.buffer[it.pos], true, nil
		}
		loaded, err := it.loadNextFile()
		if err != nil {
			return nil, false, err
		}
		if !loaded {
			return nil, false, nil
		}
		it.pos = -1
	}
}

// loadNextFile finds the next file at or beyond nextIndex, loads its records (filtered to complete blocks at
// or beyond start), and advances nextIndex. It returns false when no further file exists. A file entirely
// below start is skipped without being read; a file that yields no matching records leaves buffer empty.
func (it *walIterator) loadNextFile() (bool, error) {
	name, parsed, ok, err := findFileByMinIndex(it.wal.config.Path, it.nextIndex)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	it.nextIndex = parsed.index + 1
	it.buffer = nil

	if parsed.sealed && parsed.lastBlock < it.start {
		return true, nil // entirely below the start block; skip without reading
	}

	contents, err := readWalFile(filepath.Join(it.wal.config.Path, name))
	if err != nil {
		return false, fmt.Errorf("failed to read WAL file %s during iteration: %w", name, err)
	}
	if !contents.hasCompleteBlock {
		return true, nil
	}
	for _, entry := range contents.entries {
		if entry.BlockNumber < it.start || entry.BlockNumber > contents.lastCompleteBlock {
			continue
		}
		it.buffer = append(it.buffer, entry)
	}
	return true, nil
}

// findFileByMinIndex returns the WAL file with the smallest index greater than or equal to minIndex.
func findFileByMinIndex(directory string, minIndex uint64) (string, parsedFileName, bool, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", parsedFileName{}, false, fmt.Errorf("failed to read WAL directory %s: %w", directory, err)
	}

	var bestName string
	var best parsedFileName
	found := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		parsed, ok := parseFileName(entry.Name())
		if !ok || parsed.index < minIndex {
			continue
		}
		if !found || parsed.index < best.index {
			best = parsed
			bestName = entry.Name()
			found = true
		}
	}
	return bestName, best, found, nil
}
