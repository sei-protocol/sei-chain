package seiwal

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

// WAL files use the following naming schema:
//
// For mutable files: {file sequence}.wal.u
// For sealed files:  {file sequence}-{first index}-{last index}.wal
//
// The file sequence is a unique, monotonically increasing counter identifying the file; the first and last
// index are the record indices the sealed file spans. The on-disk serialization version is recorded in each
// file's header rather than its name.

const (
	// The file extension for unsealed (mutable) WAL files.
	walUnsealedExtension = ".wal.u"
	// The file extension for sealed (immutable) WAL files.
	walSealedExtension = ".wal"
	// The serialization version written into each file's header. Bumped if the on-disk format changes.
	walFormatVersion = byte(1)
)

// The magic prefix written at the start of every WAL file, followed by a single format-version byte.
var walFileMagic = []byte("SEIWL")

// The length of a WAL file header: magic prefix plus the format-version byte.
const walHeaderSize = 6 // len("SEIWL") + 1

var (
	unsealedFileRegex = regexp.MustCompile(`^(\d+)\.wal\.u$`)
	sealedFileRegex   = regexp.MustCompile(`^(\d+)-(\d+)-(\d+)\.wal$`)
)

// The result of parsing a WAL file name.
type parsedFileName struct {
	fileSeq    uint64
	firstIndex uint64
	lastIndex  uint64
	sealed     bool
}

// Parse a WAL file name into its components. Returns false if the name is not a WAL file name.
func parseFileName(fileName string) (parsedFileName, bool) {
	if m := sealedFileRegex.FindStringSubmatch(fileName); m != nil {
		seq, err1 := strconv.ParseUint(m[1], 10, 64)
		first, err2 := strconv.ParseUint(m[2], 10, 64)
		last, err3 := strconv.ParseUint(m[3], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return parsedFileName{}, false
		}
		return parsedFileName{fileSeq: seq, firstIndex: first, lastIndex: last, sealed: true}, true
	}
	if m := unsealedFileRegex.FindStringSubmatch(fileName); m != nil {
		seq, err := strconv.ParseUint(m[1], 10, 64)
		if err != nil {
			return parsedFileName{}, false
		}
		return parsedFileName{fileSeq: seq, sealed: false}, true
	}
	return parsedFileName{}, false
}

// Build the name of an unsealed (mutable) WAL file.
func unsealedFileName(fileSeq uint64) string {
	return fmt.Sprintf("%d%s", fileSeq, walUnsealedExtension)
}

// Build the name of a sealed WAL file.
func sealedFileName(fileSeq uint64, firstIndex uint64, lastIndex uint64) string {
	return fmt.Sprintf("%d-%d-%d%s", fileSeq, firstIndex, lastIndex, walSealedExtension)
}

// A single WAL file on disk, either the current mutable file being appended to or a sealed file.
type walFile struct {
	// The directory this file lives in.
	directory string

	// The open file handle and buffered writer. Only set for the mutable file being written this session.
	file   *os.File
	writer *bufio.Writer

	// The unique, monotonically increasing sequence number of this file.
	fileSeq uint64

	// If true, this file is sealed and rejects writes.
	sealed bool

	// The first and last record indices that appear in this file, valid only when hasRecords is true.
	firstIndex uint64
	lastIndex  uint64
	hasRecords bool

	// The number of bytes written to this file so far, including the header.
	size uint64
}

// Create a new mutable WAL file on disk, writing its header, ready to accept records.
func newWalFile(directory string, fileSeq uint64) (*walFile, error) {
	path := filepath.Join(directory, unsealedFileName(fileSeq))
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
		fileSeq:   fileSeq,
		size:      walHeaderSize,
	}, nil
}

// frameRecord wraps a payload in its on-disk framing:
// [uvarint index][uvarint payload length][payload][uint32 CRC32(index+length+payload)].
// The checksum covers the index and length prefixes as well as the payload, so a torn or corrupt index is
// detected on recovery exactly as a torn payload is.
func frameRecord(index uint64, payload []byte) []byte {
	var idxBuf [binary.MaxVarintLen64]byte
	idxN := binary.PutUvarint(idxBuf[:], index)
	var lenBuf [binary.MaxVarintLen64]byte
	lenN := binary.PutUvarint(lenBuf[:], uint64(len(payload)))

	record := make([]byte, 0, idxN+lenN+len(payload)+4)
	record = append(record, idxBuf[:idxN]...)
	record = append(record, lenBuf[:lenN]...)
	record = append(record, payload...)
	var crcBuf [4]byte
	binary.BigEndian.PutUint32(crcBuf[:], crc32.ChecksumIEEE(record))
	record = append(record, crcBuf[:]...)
	return record
}

// Append a pre-framed record (see frameRecord) with the given index to this file.
func (f *walFile) writeRecord(record []byte, index uint64) error {
	if f.sealed {
		return fmt.Errorf("cannot write to a sealed WAL file")
	}
	if _, err := f.writer.Write(record); err != nil {
		return fmt.Errorf("failed to write WAL record: %w", err)
	}
	f.size += uint64(len(record))

	if !f.hasRecords {
		f.firstIndex = index
		f.hasRecords = true
	}
	f.lastIndex = index
	return nil
}

// close releases the file handle without sealing. Used on the fatal-error path, where the file is left
// unsealed for recoverOrphans to seal (truncating any torn tail) on the next open. Idempotent.
func (f *walFile) close() error {
	if f.file == nil {
		return nil
	}
	err := f.file.Close()
	f.file = nil
	return err
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

// Seal this file: flush it, then atomically rename it to its sealed name. Every record written in-process is
// whole (records are framed and written atomically), so there is nothing to truncate. A file with no records
// (including one that never received a record) is removed rather than sealed. Idempotent. Returns the sealed
// file name, or "" if the file was removed.
func (f *walFile) seal() (string, error) {
	if f.sealed {
		return "", nil
	}
	if err := f.flush(true); err != nil {
		return "", fmt.Errorf("failed to flush before sealing: %w", err)
	}

	unsealedPath := filepath.Join(f.directory, unsealedFileName(f.fileSeq))
	if !f.hasRecords {
		if f.file != nil {
			if err := f.file.Close(); err != nil {
				return "", fmt.Errorf("failed to close WAL file: %w", err)
			}
		}
		if err := removeAndSyncDir(f.directory, unsealedFileName(f.fileSeq)); err != nil {
			return "", fmt.Errorf("failed to remove empty WAL file: %w", err)
		}
		f.sealed = true
		return "", nil
	}

	if f.file != nil {
		if err := f.file.Close(); err != nil {
			return "", fmt.Errorf("failed to close WAL file: %w", err)
		}
	}

	sealedName := sealedFileName(f.fileSeq, f.firstIndex, f.lastIndex)
	sealedPath := filepath.Join(f.directory, sealedName)
	if err := util.AtomicRename(unsealedPath, sealedPath, true); err != nil {
		return "", fmt.Errorf("failed to seal WAL file: %w", err)
	}
	f.sealed = true
	return sealedName, nil
}

// A single record read from a WAL file: its index, its payload (a sub-slice of the file's bytes), and the
// byte offset just past its framing.
type walRecord struct {
	index   uint64
	payload []byte
	end     int64
}

// The result of reading a WAL file from disk.
type walFileContents struct {
	// The parsed file name components.
	parsed parsedFileName

	// The intact records read from the file, in order. Excludes any torn trailing record.
	records []walRecord

	// The first and last record indices across the intact records, valid only when hasRecords is true.
	firstIndex uint64
	lastIndex  uint64
	hasRecords bool

	// The byte offset just past the last intact record. Data beyond this offset is a torn trailing record and
	// is discarded on recovery.
	validEnd int64
}

// Read a WAL file from disk, tolerating a torn trailing record (a crash mid-write can leave a final record
// whose index prefix, length prefix, payload, or checksum is incomplete). Any bytes past the last intact
// record are discarded; the last intact record's boundary is reported so callers can truncate the torn tail.
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

// readWalFileFromHandle reads and parses a WAL file from an already-open handle, then closes the handle.
func readWalFileFromHandle(file *os.File, parsed parsedFileName) (*walFileContents, error) {
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read WAL file %s: %w", file.Name(), err)
	}
	return parseWalFileData(data, parsed, file.Name())
}

// parseWalFileData parses the raw bytes of a WAL file (already read into memory) into its intact records,
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
	contents.validEnd = walHeaderSize

	// A torn trailing record is expected only in the mutable file after a crash, where discarding it is correct.
	// A sealed file is durable, fsync'd, and truncated to a record boundary on seal, so any framing or checksum
	// failure in it is corruption (e.g. bit-rot) that must be surfaced rather than silently discarded — otherwise
	// records past the fault vanish while the file's name keeps promising them. torn() turns each tolerated break
	// into a hard error for a sealed file.
	torn := func(offset int, reason string) error {
		if !parsed.sealed {
			return nil
		}
		return fmt.Errorf("sealed WAL file %s is corrupt: %s at offset %d", name, reason, offset)
	}

	offset := walHeaderSize
	for offset < len(data) {
		index, idxN := binary.Uvarint(data[offset:])
		if idxN <= 0 {
			if err := torn(offset, "torn record index prefix"); err != nil {
				return nil, err
			}
			break // torn or incomplete index prefix
		}
		lenStart := offset + idxN
		length, lenN := binary.Uvarint(data[lenStart:])
		if lenN <= 0 {
			if err := torn(offset, "torn record length prefix"); err != nil {
				return nil, err
			}
			break // torn or incomplete length prefix
		}
		payloadStart := lenStart + lenN
		remaining := uint64(len(data) - payloadStart) //nolint:gosec // payloadStart <= len(data), so non-negative
		if remaining < 4 || length > remaining-4 {
			if err := torn(offset, "truncated record payload or checksum"); err != nil {
				return nil, err
			}
			break // torn record: payload or checksum truncated (4 trailing bytes are the CRC32)
		}
		payloadLen := int(length) //nolint:gosec // bounded above by remaining-4, which is <= len(data)
		payloadEnd := payloadStart + payloadLen
		recordEnd := payloadEnd + 4
		gotCRC := binary.BigEndian.Uint32(data[payloadEnd:recordEnd])
		if gotCRC != crc32.ChecksumIEEE(data[offset:payloadEnd]) {
			if err := torn(offset, "record checksum mismatch"); err != nil {
				return nil, err
			}
			break // torn or corrupt record
		}

		contents.records = append(contents.records,
			walRecord{index: index, payload: data[payloadStart:payloadEnd], end: int64(recordEnd)})
		if !contents.hasRecords {
			contents.firstIndex = index
			contents.hasRecords = true
		}
		contents.lastIndex = index
		contents.validEnd = int64(recordEnd)

		offset = recordEnd
	}

	return contents, nil
}

// verifySealedContents confirms that a sealed file's intact content exactly covers the [first, last] index
// range promised by its name. A sealed file is durable and complete, so a shortfall means interior corruption
// (e.g. a record truncated to a clean boundary) that leaves parseWalFileData reading fewer records than the
// name promises, while Bounds/GetStoredRange keep reporting the full range. fileSeq is used only for error
// messages.
func verifySealedContents(contents *walFileContents, fileSeq uint64, first uint64, last uint64) error {
	if !contents.hasRecords {
		return fmt.Errorf(
			"WAL file (sequence %d) is corrupt: name promises indices [%d, %d] but no intact records were read",
			fileSeq, first, last)
	}
	if contents.firstIndex != first || contents.lastIndex != last {
		return fmt.Errorf(
			"WAL file (sequence %d) is corrupt: name promises indices [%d, %d] but content holds [%d, %d]",
			fileSeq, first, last, contents.firstIndex, contents.lastIndex)
	}
	return nil
}

// truncateAndSync truncates the file at path to size and fsyncs it, so the shorter length is durable on its
// own — before any subsequent rename. Without the fsync, a crash could persist a rename while losing the
// truncation, leaving a file whose name promises more records than its content actually holds.
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
// caller proceeds. Callers rely on this to keep the sealed-file sequence gap-free across a crash.
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
// sealed). Any torn trailing record is truncated away first, so the sealed file ends cleanly on a record
// boundary. A file left with no records is removed.
func sealOrphanFile(directory string, name string) error {
	path := filepath.Join(directory, name)
	contents, err := readWalFile(path)
	if err != nil {
		return fmt.Errorf("failed to read orphaned WAL file %s: %w", path, err)
	}

	if !contents.hasRecords {
		return removeAndSyncDir(directory, name)
	}

	if err := truncateAndSync(path, contents.validEnd); err != nil {
		return fmt.Errorf("failed to truncate orphaned WAL file %s: %w", path, err)
	}
	sealedPath := filepath.Join(directory,
		sealedFileName(contents.parsed.fileSeq, contents.firstIndex, contents.lastIndex))
	if err := util.AtomicRename(path, sealedPath, true); err != nil {
		return fmt.Errorf("failed to seal orphaned WAL file %s: %w", path, err)
	}
	return nil
}

// rollbackStraddlingFile handles the single sealed file that spans rollbackThrough: it drops every record
// beyond the rollback point and reduces the file's range accordingly. The name of a sealed file encodes its
// index range, so the reduced content and the reduced name must become durable together — a crash must never
// leave a file whose name promises more records than its content holds (the iterator bounds sealed reads by
// content and would otherwise under-yield, while Bounds trusts the name and would over-report). Because the
// reduced range means a different file name, this cannot be a single in-place rename: the kept prefix is
// written to a fresh correctly-named file via AtomicWrite (durable on its own), and only then is the old,
// larger-named file removed. A crash after the write but before the removal leaves both files under the same
// file sequence; recovery's reconcileRollbackRemnants resolves that deterministically. Files entirely beyond
// the rollback point are removed by the caller; this handles only the boundary file.
func rollbackStraddlingFile(directory string, name string, rollbackThrough uint64) error {
	path := filepath.Join(directory, name)
	parsed, ok := parseFileName(name)
	if !ok {
		return fmt.Errorf("not a WAL file: %s", name)
	}
	data, err := os.ReadFile(path) //nolint:gosec // path derived from a scanned WAL directory entry
	if err != nil {
		return fmt.Errorf("failed to read WAL file %s during rollback: %w", path, err)
	}
	contents, err := parseWalFileData(data, parsed, path)
	if err != nil {
		return fmt.Errorf("failed to parse WAL file %s during rollback: %w", path, err)
	}

	truncateTo := int64(walHeaderSize)
	var lastKept uint64
	kept := false
	for _, record := range contents.records {
		if record.index > rollbackThrough {
			break
		}
		truncateTo = record.end
		lastKept = record.index
		kept = true
	}

	if !kept {
		// The content holds no record at or below the rollback point after all; drop the whole file.
		return removeAndSyncDir(directory, name)
	}
	if lastKept == contents.lastIndex {
		return nil // nothing beyond the rollback point; leave the file untouched
	}

	// Write the kept prefix to a fresh file under its reduced name, made durable before the old file is removed.
	newName := sealedFileName(parsed.fileSeq, contents.firstIndex, lastKept)
	newPath := filepath.Join(directory, newName)
	if err := util.AtomicWrite(newPath, data[:truncateTo], true); err != nil {
		return fmt.Errorf("failed to write rolled-back WAL file %s: %w", newPath, err)
	}
	if err := removeAndSyncDir(directory, name); err != nil {
		return fmt.Errorf("failed to remove old WAL file %s during rollback: %w", path, err)
	}
	return nil
}

// reconcileRollbackRemnants resolves the one crash window left by rollbackStraddlingFile: a crash after the
// reduced file was written but before the old, larger-named file was removed leaves two sealed files sharing a
// file sequence. This can happen only from an interrupted rollback swap (healthy operation never assigns a
// sequence to two files), so the reduced file (the one with the smaller last index) is the intended survivor;
// the larger one is removed. Both files are internally name/content consistent, so the choice is made from
// names alone without reading contents. A no-op in the common case where every sealed sequence is unique.
func reconcileRollbackRemnants(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("failed to read WAL directory %s: %w", directory, err)
	}

	// The name kept for each sealed sequence so far. A duplicate sequence means an interrupted rollback swap.
	kept := make(map[uint64]parsedFileName)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		parsed, ok := parseFileName(entry.Name())
		if !ok || !parsed.sealed {
			continue
		}
		prev, seen := kept[parsed.fileSeq]
		if !seen {
			kept[parsed.fileSeq] = parsed
			continue
		}
		// Two sealed files share this sequence. Keep the one with the smaller last index (the rolled-back
		// result) and remove the other. A sealed name is a deterministic function of its parsed fields, so
		// each file's name is recoverable without tracking the raw directory entry.
		keep, drop := parsed, prev
		if prev.lastIndex <= parsed.lastIndex {
			keep, drop = prev, parsed
		}
		dropName := sealedFileName(drop.fileSeq, drop.firstIndex, drop.lastIndex)
		if err := removeAndSyncDir(directory, dropName); err != nil {
			return fmt.Errorf("failed to remove rollback remnant %s: %w", dropName, err)
		}
		kept[parsed.fileSeq] = keep
	}
	return nil
}
