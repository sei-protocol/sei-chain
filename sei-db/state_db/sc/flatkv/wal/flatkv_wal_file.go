package wal

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// FlatKV WAL files use the following naming schema (mirroring the hashlog package):
//
// For mutable files: {index}.fkvwal.u
// For sealed files:  {index}-{first block}-{last block}.fkvwal
//
// The on-disk serialization version is recorded in each file's header rather than its name.

const (
	// The file extension for unsealed (mutable) WAL files.
	walUnsealedExtension = ".fkvwal.u"
	// The file extension for sealed (immutable) WAL files.
	walSealedExtension = ".fkvwal"
	// The serialization version written into each file's header. Bumped if the on-disk format changes.
	walFormatVersion = byte(1)
)

// The magic prefix written at the start of every WAL file, followed by a single format-version byte.
var walFileMagic = []byte("FKVWAL")

// The length of a WAL file header: magic prefix plus the format-version byte.
const walHeaderSize = 7 // len("FKVWAL") + 1

var (
	unsealedFileRegex = regexp.MustCompile(`^(\d+)\.fkvwal\.u$`)
	sealedFileRegex   = regexp.MustCompile(`^(\d+)-(\d+)-(\d+)\.fkvwal$`)
)

// The result of parsing a WAL file name.
type parsedFileName struct {
	index      uint64
	firstBlock uint64
	lastBlock  uint64
	sealed     bool
}

// Parse a WAL file name into its components. Returns false if the name is not a WAL file name.
func parseFileName(fileName string) (parsedFileName, bool) {
	if m := sealedFileRegex.FindStringSubmatch(fileName); m != nil {
		index, err1 := strconv.ParseUint(m[1], 10, 64)
		first, err2 := strconv.ParseUint(m[2], 10, 64)
		last, err3 := strconv.ParseUint(m[3], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return parsedFileName{}, false
		}
		return parsedFileName{index: index, firstBlock: first, lastBlock: last, sealed: true}, true
	}
	if m := unsealedFileRegex.FindStringSubmatch(fileName); m != nil {
		index, err := strconv.ParseUint(m[1], 10, 64)
		if err != nil {
			return parsedFileName{}, false
		}
		return parsedFileName{index: index, sealed: false}, true
	}
	return parsedFileName{}, false
}

// Build the name of an unsealed (mutable) WAL file.
func unsealedFileName(index uint64) string {
	return fmt.Sprintf("%d%s", index, walUnsealedExtension)
}

// Build the name of a sealed WAL file.
func sealedFileName(index uint64, firstBlock uint64, lastBlock uint64) string {
	return fmt.Sprintf("%d-%d-%d%s", index, firstBlock, lastBlock, walSealedExtension)
}

// A single WAL file on disk, either the current mutable file being appended to or a sealed file.
type walFile struct {
	// The directory this file lives in.
	directory string

	// The open file handle and buffered writer. Only set for the mutable file being written this session.
	file   *os.File
	writer *bufio.Writer

	// The unique, monotonically increasing index of this file.
	index uint64

	// If true, this file is sealed and rejects writes.
	sealed bool

	// The first and last block numbers that appear in this file, valid only when hasBlocks is true.
	firstBlock uint64
	lastBlock  uint64
	hasBlocks  bool

	// The highest block number in this file terminated by an end-of-block marker, and the file size at that
	// marker. Valid only when hasCompleteBlock is true. On seal, any records past completeSize (an incomplete
	// trailing block) are truncated so the sealed file ends cleanly on a block boundary.
	lastCompleteBlock uint64
	completeSize      uint64
	hasCompleteBlock  bool

	// The number of bytes written to this file so far, including the header.
	size uint64
}

// Create a new mutable WAL file on disk, writing its header, ready to accept records.
func newWalFile(directory string, index uint64) (*walFile, error) {
	path := filepath.Join(directory, unsealedFileName(index))
	file, err := os.Create(path) //nolint:gosec // path derived from a validated directory
	if err != nil {
		return nil, fmt.Errorf("failed to create WAL file %s: %w", path, err)
	}

	// Persist the new directory entry so a later fsync of the file's contents (via flush) cannot be undone by a
	// power loss that drops the unsynced create. Without this, flushed data could be lost if the file is never
	// sealed (a seal fsyncs the directory via the atomic rename) before a crash.
	if err := util.SyncParentPath(path); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to fsync WAL directory after creating %s: %w", path, err)
	}

	writer := bufio.NewWriter(file)
	header := append(append([]byte(nil), walFileMagic...), walFormatVersion)
	if _, err := writer.Write(header); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to write WAL header to %s: %w", path, err)
	}

	return &walFile{
		directory: directory,
		file:      file,
		writer:    writer,
		index:     index,
		size:      walHeaderSize,
	}, nil
}

// frameRecord wraps a serialized payload in its on-disk framing:
// [uvarint payload length][payload][uint32 CRC32(payload)].
func frameRecord(payload []byte) []byte {
	var lenBuf [binary.MaxVarintLen64]byte
	lenN := binary.PutUvarint(lenBuf[:], uint64(len(payload)))

	record := make([]byte, 0, lenN+len(payload)+4)
	record = append(record, lenBuf[:lenN]...)
	record = append(record, payload...)
	var crcBuf [4]byte
	binary.BigEndian.PutUint32(crcBuf[:], crc32.ChecksumIEEE(payload))
	record = append(record, crcBuf[:]...)
	return record
}

// Append a pre-framed record (see frameRecord) for the given block number to this file. endOfBlock marks the
// record as an end-of-block marker, which advances the file's completed-block boundary.
func (f *walFile) writeRecord(record []byte, blockNumber uint64, endOfBlock bool) error {
	if f.sealed {
		return fmt.Errorf("cannot write to a sealed WAL file")
	}
	if _, err := f.writer.Write(record); err != nil {
		return fmt.Errorf("failed to write WAL record: %w", err)
	}
	f.size += uint64(len(record))

	if !f.hasBlocks {
		f.firstBlock = blockNumber
		f.hasBlocks = true
	}
	f.lastBlock = blockNumber
	if endOfBlock {
		f.lastCompleteBlock = blockNumber
		f.completeSize = f.size
		f.hasCompleteBlock = true
	}
	return nil
}

// Serialize, frame, and append a WAL entry to this file. A convenience wrapper over frameRecord and
// writeRecord for callers (rollback rewrite, tests) that hold entries rather than pre-framed bytes.
func (f *walFile) writeEntry(entry *FlatKVWalEntry) error {
	payload, err := entry.Serialize()
	if err != nil {
		return fmt.Errorf("failed to serialize WAL entry: %w", err)
	}
	return f.writeRecord(frameRecord(payload), entry.BlockNumber, entry.EndOfBlock)
}

// readIncompleteTail returns the raw framed bytes of the in-progress block — everything written past the
// last end-of-block marker — so a caller sealing the file for iteration can carry those records into a
// fresh file rather than losing them to the seal's truncation. Returns nil when the file already ends on a
// block boundary. Only meaningful when hasCompleteBlock is true (completeSize marks the last boundary).
func (f *walFile) readIncompleteTail() ([]byte, error) {
	if f.size <= f.completeSize {
		return nil, nil
	}
	if err := f.writer.Flush(); err != nil {
		return nil, fmt.Errorf("failed to flush before reading incomplete tail: %w", err)
	}
	length := f.size - f.completeSize
	buf := make([]byte, length)
	n, err := f.file.ReadAt(buf, int64(f.completeSize)) //nolint:gosec // completeSize <= size
	if err != nil && !(err == io.EOF && uint64(n) == length) {
		return nil, fmt.Errorf("failed to read incomplete tail: %w", err)
	}
	if uint64(n) != length {
		return nil, fmt.Errorf("short read of incomplete tail: read %d of %d bytes", n, length)
	}
	return buf, nil
}

// appendIncompleteTail re-appends the raw framed bytes of an in-progress block (captured by
// readIncompleteTail from the file being sealed) to this fresh mutable file, restoring the block-tracking
// state so subsequent writes and the eventual end-of-block marker behave as if the block had been written
// here all along. block is the in-progress block's number (a single block, by the write-ordering contract).
func (f *walFile) appendIncompleteTail(tail []byte, block uint64) error {
	if f.sealed {
		return fmt.Errorf("cannot write to a sealed WAL file")
	}
	if _, err := f.writer.Write(tail); err != nil {
		return fmt.Errorf("failed to write carried-forward block: %w", err)
	}
	f.size += uint64(len(tail))
	f.firstBlock = block
	f.lastBlock = block
	f.hasBlocks = true
	return nil
}

// Flush buffered data to the OS. When fsync is true, also fsync the file so the data survives power loss.
func (f *walFile) flush(fsync bool) error {
	if f.writer != nil {
		if err := f.writer.Flush(); err != nil {
			return fmt.Errorf("failed to flush WAL file: %w", err)
		}
	}
	if fsync && f.file != nil {
		if err := f.file.Sync(); err != nil {
			return fmt.Errorf("failed to fsync WAL file: %w", err)
		}
	}
	return nil
}

// Seal this file: flush it, truncate away any incomplete trailing block, then atomically rename it to its
// sealed name. A file with no complete blocks (including one that never received a record) is removed rather
// than sealed. Idempotent. Returns the sealed file name, or "" if the file was removed.
func (f *walFile) seal() (string, error) {
	if f.sealed {
		return "", nil
	}
	if err := f.flush(true); err != nil {
		return "", fmt.Errorf("failed to flush before sealing: %w", err)
	}

	unsealedPath := filepath.Join(f.directory, unsealedFileName(f.index))
	if !f.hasCompleteBlock {
		if f.file != nil {
			if err := f.file.Close(); err != nil {
				return "", fmt.Errorf("failed to close WAL file: %w", err)
			}
		}
		if err := os.Remove(unsealedPath); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to remove empty WAL file: %w", err)
		}
		f.sealed = true
		return "", nil
	}

	if f.file != nil {
		// Drop any records past the last end-of-block marker so the sealed file ends on a block boundary.
		if f.size > f.completeSize {
			if err := f.file.Truncate(int64(f.completeSize)); err != nil { //nolint:gosec // completeSize <= size
				return "", fmt.Errorf("failed to truncate incomplete trailing block: %w", err)
			}
			if err := f.file.Sync(); err != nil {
				return "", fmt.Errorf("failed to fsync WAL file after truncation: %w", err)
			}
		}
		if err := f.file.Close(); err != nil {
			return "", fmt.Errorf("failed to close WAL file: %w", err)
		}
	}

	sealedName := sealedFileName(f.index, f.firstBlock, f.lastCompleteBlock)
	sealedPath := filepath.Join(f.directory, sealedName)
	if err := util.AtomicRename(unsealedPath, sealedPath, true); err != nil {
		return "", fmt.Errorf("failed to seal WAL file: %w", err)
	}
	f.sealed = true
	return sealedName, nil
}

// The result of reading a WAL file from disk.
type walFileContents struct {
	// The parsed file name components.
	parsed parsedFileName

	// The intact entries read from the file, in order. Excludes any torn trailing record.
	entries []*FlatKVWalEntry

	// The first and last block numbers across the intact entries, valid only when hasBlocks is true.
	firstBlock uint64
	lastBlock  uint64
	hasBlocks  bool

	// The byte offset just past the last record terminated by an end-of-block marker. Data beyond this offset
	// belongs to an incomplete (uncommitted) block, or is a torn trailing record, and is discarded on recovery.
	lastCompleteBlockOffset int64

	// The highest block number terminated by an end-of-block marker, valid only when hasCompleteBlock is true.
	lastCompleteBlock uint64
	hasCompleteBlock  bool

	// One entry per end-of-block marker, recording the marker's block number and the byte offset just past its
	// record. Ordered by ascending offset. Used to truncate the file at a block boundary (e.g. for rollback).
	blockBoundaries []blockBoundary
}

// The byte offset just past an end-of-block marker for a given block number.
type blockBoundary struct {
	block  uint64
	offset int64
}

// Read a WAL file from disk, tolerating a torn trailing record (a crash mid-write can leave a final record
// whose length prefix, payload, or checksum is incomplete). Any bytes past the last intact record are
// discarded; the last intact record's boundaries are reported so callers can recover incomplete tail blocks.
func readWalFile(path string) (*walFileContents, error) {
	name := filepath.Base(path)
	parsed, ok := parseFileName(name)
	if !ok {
		return nil, fmt.Errorf("not a WAL file: %s", name)
	}

	data, err := os.ReadFile(path) //nolint:gosec // caller-supplied path
	if err != nil {
		return nil, fmt.Errorf("failed to read WAL file %s: %w", path, err)
	}

	return parseWalFileData(data, parsed, path)
}

// readWalFileFromHandle reads and parses a WAL file from an already-open handle, then closes the handle. The
// mutable file's handle is pre-opened on the writer goroutine (see startIterator) so a later rename cannot
// invalidate it; sealed files are opened lazily by name (see walIterator.openFile). Either way the heavy read
// happens here, on the iterator's reader goroutine. parsed carries the file-name components for error context.
func readWalFileFromHandle(file *os.File, parsed parsedFileName) (*walFileContents, error) {
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read WAL file %s: %w", file.Name(), err)
	}
	return parseWalFileData(data, parsed, file.Name())
}

// parseWalFileData parses the raw bytes of a WAL file (already read into memory) into its intact entries,
// tolerating a torn trailing record. name is used only for error messages. It is shared by readWalFile (which
// reads by path) and the iterator (which reads through a file handle opened on the writer goroutine).
func parseWalFileData(data []byte, parsed parsedFileName, name string) (*walFileContents, error) {
	contents := &walFileContents{parsed: parsed}

	if len(data) < walHeaderSize {
		// A file too short to even contain a header carries no committed data.
		return contents, nil
	}
	if !bytes.Equal(data[:len(walFileMagic)], walFileMagic) {
		return nil, fmt.Errorf("WAL file %s has an invalid magic prefix", name)
	}
	if version := data[len(walFileMagic)]; version != walFormatVersion {
		return nil, fmt.Errorf("WAL file %s has unsupported format version %d", name, version)
	}
	contents.lastCompleteBlockOffset = walHeaderSize

	offset := walHeaderSize
	for offset < len(data) {
		length, lenN := binary.Uvarint(data[offset:])
		if lenN <= 0 {
			break // torn or incomplete length prefix
		}
		payloadStart := offset + lenN
		remaining := uint64(len(data) - payloadStart) //nolint:gosec // payloadStart <= len(data), so non-negative
		if remaining < 4 || length > remaining-4 {
			break // torn record: payload or checksum truncated (4 trailing bytes are the CRC32)
		}
		payloadLen := int(length) //nolint:gosec // bounded above by remaining-4, which is <= len(data)
		payload := data[payloadStart : payloadStart+payloadLen]
		recordEnd := payloadStart + payloadLen + 4
		gotCRC := binary.BigEndian.Uint32(data[payloadStart+payloadLen : recordEnd])
		if gotCRC != crc32.ChecksumIEEE(payload) {
			break // torn or corrupt record
		}

		entry, entryOK, err := DeserializeFlatKVWalEntry(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize record in %s: %w", name, err)
		}
		if !entryOK {
			break // torn payload
		}

		contents.entries = append(contents.entries, entry)
		if !contents.hasBlocks {
			contents.firstBlock = entry.BlockNumber
			contents.hasBlocks = true
		}
		contents.lastBlock = entry.BlockNumber
		if entry.EndOfBlock {
			contents.lastCompleteBlockOffset = int64(recordEnd)
			contents.lastCompleteBlock = entry.BlockNumber
			contents.hasCompleteBlock = true
			contents.blockBoundaries = append(contents.blockBoundaries,
				blockBoundary{block: entry.BlockNumber, offset: int64(recordEnd)})
		}

		offset = recordEnd
	}

	return contents, nil
}

// truncateAndSync truncates the file at path to size and fsyncs it, so the shorter length is durable on its
// own — before any subsequent rename. Without the fsync, a crash could persist a rename while losing the
// truncation, leaving a file whose name promises fewer blocks than its content actually holds.
func truncateAndSync(path string, size int64) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0) //nolint:gosec // caller-supplied path
	if err != nil {
		return fmt.Errorf("failed to open %s for truncation: %w", path, err)
	}
	if err := file.Truncate(size); err != nil {
		_ = file.Close()
		return fmt.Errorf("failed to truncate %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("failed to fsync %s after truncation: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close %s after truncation: %w", path, err)
	}
	return nil
}

// removeAndSyncDir removes the named file and fsyncs its parent directory, so the removal is durable before the
// caller proceeds. Callers rely on this to keep the sealed-file index sequence gap-free across a crash.
func removeAndSyncDir(directory string, name string) error {
	path := filepath.Join(directory, name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove WAL file %s: %w", path, err)
	}
	if err := util.SyncParentPath(path); err != nil {
		return fmt.Errorf("failed to fsync directory after removing %s: %w", path, err)
	}
	return nil
}

// Seal an orphaned mutable file discovered on disk at startup (left behind by a crash before it could be
// sealed). Any incomplete trailing block (records not terminated by an end-of-block marker) or torn trailing
// record is truncated away first, so the sealed file ends cleanly on a block boundary. A file left with no
// complete blocks is removed.
func sealOrphanFile(directory string, name string) error {
	path := filepath.Join(directory, name)
	contents, err := readWalFile(path)
	if err != nil {
		return fmt.Errorf("failed to read orphaned WAL file %s: %w", path, err)
	}

	if !contents.hasCompleteBlock {
		return removeAndSyncDir(directory, name)
	}

	if err := truncateAndSync(path, contents.lastCompleteBlockOffset); err != nil {
		return fmt.Errorf("failed to truncate orphaned WAL file %s: %w", path, err)
	}
	sealedPath := filepath.Join(directory,
		sealedFileName(contents.parsed.index, contents.firstBlock, contents.lastCompleteBlock))
	if err := util.AtomicRename(path, sealedPath, true); err != nil {
		return fmt.Errorf("failed to seal orphaned WAL file %s: %w", path, err)
	}
	return nil
}

// rollbackStraddlingFile handles the single sealed file that spans rollbackThrough: it truncates away every
// block beyond the rollback point and renames the file to reflect the reduced range. The truncation is fsynced
// before the rename (see truncateAndSync), so a crash can never leave the file's content holding blocks past
// the rollback point once the rename is durable — the iterator, which bounds sealed reads by content, would
// otherwise replay the discarded blocks. Files entirely beyond the rollback point are removed by the caller;
// this handles only the boundary file.
func rollbackStraddlingFile(directory string, name string, rollbackThrough uint64) error {
	path := filepath.Join(directory, name)
	contents, err := readWalFile(path)
	if err != nil {
		return fmt.Errorf("failed to read WAL file %s during rollback: %w", path, err)
	}

	truncateTo := int64(walHeaderSize)
	var lastKept uint64
	kept := false
	for _, boundary := range contents.blockBoundaries {
		if boundary.block > rollbackThrough {
			break
		}
		truncateTo = boundary.offset
		lastKept = boundary.block
		kept = true
	}

	if !kept {
		// The content holds no complete block at or below the rollback point after all; drop the whole file.
		return removeAndSyncDir(directory, name)
	}
	if lastKept == contents.lastBlock {
		return nil // nothing beyond the rollback point; leave the file untouched
	}

	if err := truncateAndSync(path, truncateTo); err != nil {
		return fmt.Errorf("failed to truncate WAL file %s during rollback: %w", path, err)
	}
	newPath := filepath.Join(directory,
		sealedFileName(contents.parsed.index, contents.firstBlock, lastKept))
	if newPath == path {
		return nil
	}
	if err := util.AtomicRename(path, newPath, true); err != nil {
		return fmt.Errorf("failed to rename WAL file %s during rollback: %w", path, err)
	}
	return nil
}
