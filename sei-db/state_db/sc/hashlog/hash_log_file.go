package hashlog

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Hashlog files use the following naming schema:
//
// For mutable files: {index}-{version}.hlog.u
// For sealed files: {index}-{first block}-{last block}-{version}.hlog
//
// The version is sanitized at construction (see sanitizeVersion) so it never contains a "-", which keeps the
// "-" field separator unambiguous.

const (
	// The file extension for unsealed hash log files.
	HashLogUnsealedExtension = ".hlog.u"
	// The file extension for sealed hash log files.
	HashLogSealedExtension = ".hlog"
)

var (
	unsealedFileRegex = regexp.MustCompile(`^(\d+)-([A-Za-z0-9._]+)\.hlog\.u$`)
	sealedFileRegex   = regexp.MustCompile(`^(\d+)-(\d+)-(\d+)-([A-Za-z0-9._]+)\.hlog$`)
)

type hashLogFile struct {
	// The directory this file lives in.
	directory string

	// The open file handle and buffered writer. Only set for mutable files being written this session;
	// nil for files read back from disk.
	file   *os.File
	writer *bufio.Writer

	// The hash types (i.e. the CSV column order) for this file.
	hashTypes []string

	// The software version recorded in this file's name.
	version string

	// The hash logs contained within this file. Only populated when the file is read from disk.
	logs []HashLog

	// If false, this file is mutable and can accept writes. If true, new writes are rejected.
	// Files are only unsealed if they were created fresh during the current session. Files on
	// disk at startup time are always considered sealed.
	sealed bool

	// The first block number that appears in this file, or 0 if this file is empty.
	firstBlockIndex uint64

	// The last block number that appears in this file, or 0 if this file is empty.
	lastBlockIndex uint64

	// Whether this file contains any blocks.
	hasBlocks bool

	// Each hash log file is given a unique index which monotonically increases with each new file.
	index uint64

	// The number of bytes written to this file so far (including the header).
	size uint64

	// Whether the header line has been written to the mutable file yet.
	headerWritten bool
}

// The result of parsing a hash log file name.
type parsedFileName struct {
	index      uint64
	firstBlock uint64
	lastBlock  uint64
	version    string
	sealed     bool
}

// Parse a hash log file name into its components. Returns false if the name is not a hash log file name.
func parseFileName(fileName string) (parsedFileName, bool) {
	if m := sealedFileRegex.FindStringSubmatch(fileName); m != nil {
		index, err1 := strconv.ParseUint(m[1], 10, 64)
		first, err2 := strconv.ParseUint(m[2], 10, 64)
		last, err3 := strconv.ParseUint(m[3], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return parsedFileName{}, false
		}
		return parsedFileName{index: index, firstBlock: first, lastBlock: last, version: m[4], sealed: true}, true
	}
	if m := unsealedFileRegex.FindStringSubmatch(fileName); m != nil {
		index, err := strconv.ParseUint(m[1], 10, 64)
		if err != nil {
			return parsedFileName{}, false
		}
		return parsedFileName{index: index, version: m[2], sealed: false}, true
	}
	return parsedFileName{}, false
}

// Build the name of an unsealed (mutable) hash log file.
func unsealedFileName(index uint64, version string) string {
	return fmt.Sprintf("%d-%s%s", index, version, HashLogUnsealedExtension)
}

// Build the name of a sealed hash log file.
func sealedFileName(index uint64, firstBlock uint64, lastBlock uint64, version string) string {
	return fmt.Sprintf("%d-%d-%d-%s%s", index, firstBlock, lastBlock, version, HashLogSealedExtension)
}

// Check if a file name matches the expected pattern for hash log files, and if so, whether it's sealed or unsealed.
func isHashLogFileName(fileName string) (isHashLogFile bool, isSealed bool) {
	parsed, ok := parseFileName(fileName)
	if !ok {
		return false, false
	}
	return true, parsed.sealed
}

// Parse the block numbers and version from a hash log file name. For unsealed names (which carry no block
// range) the returned block numbers are both 0.
func parseBlockNumbersFromFileName(
	fileName string,
) (firstBlockNumber uint64, lastBlockNumber uint64, version string, err error) {
	parsed, ok := parseFileName(fileName)
	if !ok {
		return 0, 0, "", fmt.Errorf("not a hash log file name: %s", fileName)
	}
	return parsed.firstBlock, parsed.lastBlock, parsed.version, nil
}

// Create a new mutable hash log file on disk, ready to accept writes.
func newHashLogFile(directory string, index uint64, version string, hashTypes []string) (*hashLogFile, error) {
	path := filepath.Join(directory, unsealedFileName(index, version))
	file, err := os.Create(path) //nolint:gosec // path derived from a validated directory and sanitized version
	if err != nil {
		return nil, fmt.Errorf("failed to create hash log file %s: %w", path, err)
	}
	return &hashLogFile{
		directory: directory,
		file:      file,
		writer:    bufio.NewWriter(file),
		hashTypes: hashTypes,
		version:   version,
		index:     index,
	}, nil
}

// Parse the header line of a hash log file, returning the file's hash types (column order).
func parseHeader(header string) ([]string, error) {
	fields := strings.Split(header, ",")
	if len(fields) < 1 || fields[0] != blockNumberHeader {
		return nil, fmt.Errorf("unexpected header %q", header)
	}
	return fields[1:], nil
}

// Read a hash log file from disk.
//
// Tolerant of a truncated final record: a crash mid-write can leave a final line without its terminating
// newline. Because every complete record ends in "\n", any trailing bytes not followed by a newline are
// treated as a partial write and discarded. (A sealed file recovered from such a crash may physically retain
// those trailing bytes, but every reader discards them, so it is harmless.)
func ReadHashLogFile(path string) (*hashLogFile, error) {
	name := filepath.Base(path)
	parsed, ok := parseFileName(name)
	if !ok {
		return nil, fmt.Errorf("not a hash log file: %s", name)
	}

	data, err := os.ReadFile(path) //nolint:gosec // caller-supplied path
	if err != nil {
		return nil, fmt.Errorf("failed to read hash log file %s: %w", path, err)
	}

	file := &hashLogFile{
		directory: filepath.Dir(path),
		version:   parsed.version,
		index:     parsed.index,
		sealed:    parsed.sealed,
	}

	lines := strings.Split(string(data), "\n")
	// Everything except the last element is a complete, newline-terminated line. The last element is either
	// "" (a clean trailing newline) or a torn partial record; discard it in both cases.
	if len(lines) <= 1 {
		return file, nil
	}
	completeLines := lines[:len(lines)-1]

	hashTypes, err := parseHeader(completeLines[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse header of %s: %w", path, err)
	}
	file.hashTypes = hashTypes

	for _, line := range completeLines[1:] {
		hashLog, err := hashLogFromCSV(hashTypes, line)
		if err != nil {
			return nil, fmt.Errorf("failed to parse record in %s: %w", path, err)
		}
		hashLog.Version = parsed.version
		file.logs = append(file.logs, *hashLog)
	}

	if len(file.logs) > 0 {
		file.hasBlocks = true
		file.firstBlockIndex = file.logs[0].BlockNumber
		file.lastBlockIndex = file.logs[len(file.logs)-1].BlockNumber
	}

	return file, nil
}

// Write the next HashLog to this file. If the file is sealed, this returns an error.
func (h *hashLogFile) write(hashLog *HashLog) error {
	if h.sealed {
		return fmt.Errorf("cannot write to a sealed hash log file")
	}

	if !h.headerWritten {
		n, err := h.writer.WriteString(hashLogHeaders(h.hashTypes) + "\n")
		if err != nil {
			return fmt.Errorf("failed to write header: %w", err)
		}
		h.size += uint64(n) //nolint:gosec // WriteString returns a non-negative byte count
		h.headerWritten = true
	}

	n, err := h.writer.WriteString(hashLog.toCSV(h.hashTypes) + "\n")
	if err != nil {
		return fmt.Errorf("failed to write hash log: %w", err)
	}
	h.size += uint64(n) //nolint:gosec // WriteString returns a non-negative byte count

	if !h.hasBlocks {
		h.firstBlockIndex = hashLog.BlockNumber
		h.hasBlocks = true
	}
	h.lastBlockIndex = hashLog.BlockNumber

	return nil
}

// Close this file, flushing any buffered data to disk and marking it as sealed. Sealing renames the file from
// its unsealed name to its sealed name via an atomic rename. An empty file (one that never received a write) is
// removed rather than sealed. Idempotent.
func (h *hashLogFile) close() error {
	if h.sealed {
		return nil
	}

	if h.writer != nil {
		if err := h.writer.Flush(); err != nil {
			return fmt.Errorf("failed to flush hash log file: %w", err)
		}
	}
	if h.file != nil {
		if err := h.file.Sync(); err != nil {
			return fmt.Errorf("failed to sync hash log file: %w", err)
		}
		if err := h.file.Close(); err != nil {
			return fmt.Errorf("failed to close hash log file: %w", err)
		}
	}

	unsealedPath := filepath.Join(h.directory, unsealedFileName(h.index, h.version))

	if !h.hasBlocks {
		// Nothing was ever written; drop the empty file rather than sealing a meaningless 0-0 range.
		if err := os.Remove(unsealedPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove empty hash log file: %w", err)
		}
		h.sealed = true
		return nil
	}

	sealedPath := filepath.Join(h.directory, sealedFileName(h.index, h.firstBlockIndex, h.lastBlockIndex, h.version))
	if err := atomicRename(unsealedPath, sealedPath, true); err != nil {
		return fmt.Errorf("failed to seal hash log file: %w", err)
	}
	h.sealed = true
	return nil
}

// Used to seal a hash log file discovered on disk with the ".hlog.u" extension. This can happen if a node
// crashes before it manages to seal a hashlog file.
func sealHashLog(path string) error {
	file, err := ReadHashLogFile(path)
	if err != nil {
		return fmt.Errorf("failed to read orphaned hash log file %s: %w", path, err)
	}

	if !file.hasBlocks {
		// An empty or header-only orphan carries no useful data; just remove it.
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove empty orphaned hash log file %s: %w", path, err)
		}
		return nil
	}

	sealedPath := filepath.Join(
		filepath.Dir(path),
		sealedFileName(file.index, file.firstBlockIndex, file.lastBlockIndex, file.version),
	)
	if err := atomicRename(path, sealedPath, true); err != nil {
		return fmt.Errorf("failed to seal orphaned hash log file %s: %w", path, err)
	}
	return nil
}
