package wal

import (
	"fmt"
	"os"
	"path/filepath"
)

var _ FlatKVWalIterator = (*walIterator)(nil)

// walIterator iterates the WAL a block at a time, in ascending block order. All records written for a block
// (one per Write call) plus its end-of-block marker are coalesced into a single entry whose Changeset is the
// concatenation, in write order, of every record's changesets. It loads one file at a time from disk, so its
// memory use is bounded by a single WAL file (plus the block being assembled). It re-lists the directory as it
// advances between files, so files rotated (mutable sealed) or created after construction are still observed.
type walIterator struct {
	// The WAL this iterator reads from. Set to nil by Close so the read lease is released exactly once.
	wal *flatKVWalImpl

	start uint64

	// The block pinned as this iterator's read lease, released on Close.
	pinnedBlock uint64

	// The smallest file index not yet consumed.
	nextIndex uint64

	// The records loaded from the current file, filtered to complete blocks at or beyond start.
	buffer []*FlatKVWalEntry
	// The position within buffer; -1 before the first record is read.
	pos int

	// The coalesced block entry returned by Entry, set by the most recent successful Next.
	result *FlatKVWalEntry

	// Set once no further blocks remain.
	done bool
}

// newWalIterator creates an iterator over wal starting at startingBlockNumber. pinnedBlock is the read lease
// registered on the iterator's behalf, released by Close.
func newWalIterator(wal *flatKVWalImpl, startingBlockNumber uint64, pinnedBlock uint64) *walIterator {
	return &walIterator{
		wal:         wal,
		start:       startingBlockNumber,
		pinnedBlock: pinnedBlock,
		pos:         -1,
	}
}

func (it *walIterator) Next() (bool, error) {
	if it.done {
		return false, nil
	}

	var block *FlatKVWalEntry
	for {
		record, ok, err := it.nextRecord()
		if err != nil {
			it.done = true
			return false, fmt.Errorf("failed to advance WAL iterator: %w", err)
		}
		if !ok {
			// End of stream. A complete block always ends with an end-of-block marker, so reaching here
			// mid-block should not happen; emit any assembled changes defensively rather than dropping them.
			it.done = true
			if block != nil {
				it.result = block
				return true, nil
			}
			return false, nil
		}

		if block == nil {
			block = &FlatKVWalEntry{BlockNumber: record.BlockNumber}
		}
		if record.EndOfBlock {
			it.result = block
			return true, nil
		}
		block.Changeset = append(block.Changeset, record.Changeset...)
	}
}

func (it *walIterator) Entry() *FlatKVWalEntry {
	return it.result
}

func (it *walIterator) Close() error {
	if it.wal != nil {
		it.wal.unpinBlock(it.pinnedBlock)
		it.wal = nil // release the lease exactly once, even if Close is called repeatedly
	}
	it.buffer = nil
	it.result = nil
	it.done = true
	return nil
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
