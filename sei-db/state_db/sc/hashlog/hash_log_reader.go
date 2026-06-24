package hashlog

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// A hash log file in an archive, paired with its parsed name.
type archiveFile struct {
	name   string
	parsed parsedFileName
}

// List the hash log files in an archive directory, sorted by file index (ascending).
func listArchiveFiles(path string) ([]archiveFile, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read archive directory %s: %w", path, err)
	}
	var files []archiveFile
	for _, entry := range entries {
		parsed, ok := parseFileName(entry.Name())
		if !ok {
			continue
		}
		files = append(files, archiveFile{name: entry.Name(), parsed: parsed})
	}
	sort.Slice(files, func(i int, j int) bool {
		return files[i].parsed.index < files[j].parsed.index
	})
	return files, nil
}

// Looks for a particular block in a hash log archive. May return multiple hashes if the block
// number in question was executed multiple times (i.e. if there was a rollback). Results are returned in
// archive order (by file index, then by position within a file).
func ReadHashForBlock(path string, block uint64) ([]*HashLog, error) {
	files, err := listArchiveFiles(path)
	if err != nil {
		return nil, err
	}

	var result []*HashLog
	for _, file := range files {
		// Sealed files carry their block range in the name, so we can skip files that can't contain the block
		// without reading them.
		if file.parsed.sealed && (block < file.parsed.firstBlock || block > file.parsed.lastBlock) {
			continue
		}
		parsed, err := ReadHashLogFile(filepath.Join(path, file.name))
		if err != nil {
			return nil, err
		}
		for i := range parsed.logs {
			if parsed.logs[i].BlockNumber == block {
				log := parsed.logs[i]
				result = append(result, &log)
			}
		}
	}
	return result, nil
}

// A set of hashes from two different hash log archives.
type HashLogPair struct {
	// A hash from archive A. May actually be several hashes if A contains multiple reports for the block in question.
	HashesFromA []*HashLog

	// A hash from archive B. May actually be several hashes if A contains multiple reports for the block in question.
	HashesFromB []*HashLog
}

// fileMeta describes a hash log file's block range without holding its contents.
type fileMeta struct {
	name       string
	index      uint64
	firstBlock uint64
	lastBlock  uint64
}

// A loaded file's records, indexed by block number for O(1) lookup.
type loadedFile struct {
	lastBlock uint64
	byBlock   map[uint64][]*HashLog
}

// archiveReader streams the HashLogs of an archive in increasing block-number order without holding the whole
// archive in memory. As the caller advances the cursor, files are loaded when the cursor reaches their first
// block and evicted once it passes their last block, so memory is bounded by the number of files whose ranges
// overlap the cursor (one or two in the common case; a few when rollbacks create overlapping ranges).
type archiveReader struct {
	dir string

	// File metadata sorted by firstBlock (then index), used to activate files as the cursor advances.
	byFirst []fileMeta

	// Index into byFirst of the next file to activate.
	nextIdx int

	// Currently-loaded files, keyed by file index.
	active map[uint64]*loadedFile

	hasBlocks bool
	minBlock  uint64
	maxBlock  uint64
}

func newArchiveReader(dir string) (*archiveReader, error) {
	files, err := listArchiveFiles(dir)
	if err != nil {
		return nil, err
	}

	r := &archiveReader{dir: dir, active: make(map[uint64]*loadedFile)}
	for _, af := range files {
		meta, ok, err := fileMetaFor(dir, af)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		r.byFirst = append(r.byFirst, meta)
		if !r.hasBlocks {
			r.hasBlocks = true
			r.minBlock = meta.firstBlock
			r.maxBlock = meta.lastBlock
		} else {
			r.minBlock = min(r.minBlock, meta.firstBlock)
			r.maxBlock = max(r.maxBlock, meta.lastBlock)
		}
	}

	sort.Slice(r.byFirst, func(i int, j int) bool {
		if r.byFirst[i].firstBlock != r.byFirst[j].firstBlock {
			return r.byFirst[i].firstBlock < r.byFirst[j].firstBlock
		}
		return r.byFirst[i].index < r.byFirst[j].index
	})
	return r, nil
}

// fileMetaFor returns the block range of a file. Sealed files report their range from the name without being
// read; an unsealed file is read once to discover its range. Returns ok=false for files with no blocks.
func fileMetaFor(dir string, af archiveFile) (fileMeta, bool, error) {
	if af.parsed.sealed {
		return fileMeta{
			name:       af.name,
			index:      af.parsed.index,
			firstBlock: af.parsed.firstBlock,
			lastBlock:  af.parsed.lastBlock,
		}, true, nil
	}
	file, err := ReadHashLogFile(filepath.Join(dir, af.name))
	if err != nil {
		return fileMeta{}, false, err
	}
	if !file.hasBlocks {
		return fileMeta{}, false, nil
	}
	return fileMeta{
		name:       af.name,
		index:      af.parsed.index,
		firstBlock: file.firstBlockIndex,
		lastBlock:  file.lastBlockIndex,
	}, true, nil
}

// at returns every HashLog recorded for the given block, in file-index order. The block must be passed in
// non-decreasing order across calls.
func (r *archiveReader) at(block uint64) ([]*HashLog, error) {
	// Activate files the cursor has now reached.
	for r.nextIdx < len(r.byFirst) && r.byFirst[r.nextIdx].firstBlock <= block {
		meta := r.byFirst[r.nextIdx]
		r.nextIdx++
		if meta.lastBlock < block {
			// Entirely below the cursor: only happens when the scan starts mid-range (CompareHashesInRange),
			// where files before the requested window never need to be read.
			continue
		}
		loaded, err := r.loadFile(meta)
		if err != nil {
			return nil, err
		}
		r.active[meta.index] = loaded
	}

	// Evict files the cursor has passed.
	for idx, loaded := range r.active {
		if loaded.lastBlock < block {
			delete(r.active, idx)
		}
	}

	if len(r.active) == 0 {
		return nil, nil
	}

	// Collect occurrences in file-index order for deterministic results.
	indices := make([]uint64, 0, len(r.active))
	for idx := range r.active {
		indices = append(indices, idx)
	}
	sort.Slice(indices, func(i int, j int) bool { return indices[i] < indices[j] })

	var result []*HashLog
	for _, idx := range indices {
		result = append(result, r.active[idx].byBlock[block]...)
	}
	return result, nil
}

func (r *archiveReader) loadFile(meta fileMeta) (*loadedFile, error) {
	file, err := ReadHashLogFile(filepath.Join(r.dir, meta.name))
	if err != nil {
		return nil, err
	}
	byBlock := make(map[uint64][]*HashLog)
	for i := range file.logs {
		log := file.logs[i]
		byBlock[log.BlockNumber] = append(byBlock[log.BlockNumber], &log)
	}
	return &loadedFile{lastBlock: meta.lastBlock, byBlock: byBlock}, nil
}

// Compare two hash log archives, looking for differences between them. Returns information for blocks with
// deviant values, from lowest to highest. Covers every block present in either archive; to restrict the
// comparison to a sub-range, use CompareHashesInRange.
//
// The comparison streams from the lowest block number to the highest, loading individual files on demand and
// holding only those whose block ranges overlap the current cursor. It does not load either archive fully into
// memory, so it is viable for archives far larger than RAM.
func CompareHashes(
	// the path to the first archive
	pathA string,
	// the path to the second archive
	pathB string,
	// the maximum number of diffs to return, or -1 if all diffs should be returned. If nodes diverge
	// and run for a long time, the number of deviant blocks may be very large. This always returns the first
	// diffs encountered if it does not return all diffs.
	maxDiffCount int,
) ([]*HashLogPair, error) {
	readerA, readerB, err := openArchiveReaders(pathA, pathB)
	if err != nil {
		return nil, err
	}
	lowBlock, highBlock, ok := globalBlockRange(readerA, readerB)
	if !ok {
		return nil, nil
	}
	return compareBlockRange(readerA, readerB, lowBlock, highBlock, maxDiffCount)
}

// CompareHashesInRange is CompareHashes restricted to the inclusive block range [lowBlock, highBlock], for
// zooming in on a region of interest. The requested window is clamped to the blocks actually present in the
// archives, and files entirely below the window are never read, so it is cheap even far from block zero.
func CompareHashesInRange(
	pathA string,
	pathB string,
	// the lowest block number to compare (inclusive)
	lowBlock uint64,
	// the highest block number to compare (inclusive)
	highBlock uint64,
	// the maximum number of diffs to return, or -1 for all (see CompareHashes)
	maxDiffCount int,
) ([]*HashLogPair, error) {
	if lowBlock > highBlock {
		return nil, fmt.Errorf("lowBlock (%d) must not exceed highBlock (%d)", lowBlock, highBlock)
	}
	readerA, readerB, err := openArchiveReaders(pathA, pathB)
	if err != nil {
		return nil, err
	}
	globalLow, globalHigh, ok := globalBlockRange(readerA, readerB)
	if !ok {
		return nil, nil
	}
	// Clamp the requested window to the blocks actually present; nothing outside that range can differ.
	low := max(lowBlock, globalLow)
	high := min(highBlock, globalHigh)
	if low > high {
		return nil, nil
	}
	return compareBlockRange(readerA, readerB, low, high, maxDiffCount)
}

// openArchiveReaders opens both archives for streaming comparison.
func openArchiveReaders(pathA string, pathB string) (*archiveReader, *archiveReader, error) {
	readerA, err := newArchiveReader(pathA)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open archive A: %w", err)
	}
	readerB, err := newArchiveReader(pathB)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open archive B: %w", err)
	}
	return readerA, readerB, nil
}

// globalBlockRange returns the inclusive block range spanned by either archive. ok is false if both are empty.
func globalBlockRange(readerA *archiveReader, readerB *archiveReader) (low uint64, high uint64, ok bool) {
	switch {
	case readerA.hasBlocks && readerB.hasBlocks:
		return min(readerA.minBlock, readerB.minBlock), max(readerA.maxBlock, readerB.maxBlock), true
	case readerA.hasBlocks:
		return readerA.minBlock, readerA.maxBlock, true
	case readerB.hasBlocks:
		return readerB.minBlock, readerB.maxBlock, true
	default:
		return 0, 0, false
	}
}

// compareBlockRange streams the comparison over the inclusive range [lowBlock, highBlock], which the caller
// must have already validated and clamped. Both readers are advanced in lockstep, in non-decreasing block
// order, as required by archiveReader.at.
func compareBlockRange(
	readerA *archiveReader,
	readerB *archiveReader,
	lowBlock uint64,
	highBlock uint64,
	maxDiffCount int,
) ([]*HashLogPair, error) {
	var diffs []*HashLogPair
	for block := lowBlock; block <= highBlock; block++ {
		hashesA, err := readerA.at(block)
		if err != nil {
			return nil, fmt.Errorf("failed to read block %d from archive A: %w", block, err)
		}
		hashesB, err := readerB.at(block)
		if err != nil {
			return nil, fmt.Errorf("failed to read block %d from archive B: %w", block, err)
		}
		if hashLogsDiffer(hashesA, hashesB) {
			if maxDiffCount >= 0 && len(diffs) >= maxDiffCount {
				break
			}
			diffs = append(diffs, &HashLogPair{HashesFromA: hashesA, HashesFromB: hashesB})
		}
	}
	return diffs, nil
}

// hashLogsDiffer reports whether the reports for a single block differ between two archives.
func hashLogsDiffer(a []*HashLog, b []*HashLog) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if hashMapsDiffer(a[i].Hashes, b[i].Hashes) {
			return true
		}
	}
	return false
}

// hashMapsDiffer compares two type->hash maps over the union of their keys. A missing key compares as a nil
// hash, so a nil hash and an absent type are treated as equal.
func hashMapsDiffer(a map[string][]byte, b map[string][]byte) bool {
	keys := make(map[string]struct{}, len(a)+len(b))
	for key := range a {
		keys[key] = struct{}{}
	}
	for key := range b {
		keys[key] = struct{}{}
	}
	for key := range keys {
		if !bytes.Equal(a[key], b[key]) {
			return true
		}
	}
	return false
}
