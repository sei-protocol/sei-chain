// Crash-safe A/B file persistence.
//
// # A/B File Strategy
//
// We use an A/B file pair (<prefix>_a.pb/<prefix>_b.pb) instead of the traditional
// temp-file-then-rename approach:
//
//   - Traditional approach: write to temp file, fsync, rename to target, fsync directory
//   - A/B approach: alternate writes between <prefix>_a.pb and <prefix>_b.pb, fsync after write
//
// Why A/B files?
//
//   - Traditional temp+rename requires directory sync after file sync to ensure
//     the rename (directory entry update) is durable — that's an extra disk operation
//   - With A/B files, we only need one file sync per write
//   - Directory sync is only needed when a file is first created (at most twice total)
//   - Safety comes from redundancy: while writing to A, B is untouched; while writing
//     to B, A is untouched. A crash only corrupts the file being written.
//   - On load, we read both files and pick the one with the higher seq
//
// # Recovery Behavior
//
//   - Fresh start (files don't exist): Returns ErrNoData
//   - One file corrupt (e.g. crash during write): Uses the other file; logged at WARN
//   - Both files corrupt: Returns error (real data loss)
//   - OS-level errors (permission denied, I/O): Propagated immediately
//
// # Write Behavior
//
//   - State directory must already exist (we do not create it).
//   - Writes are synchronous (fsync after each write).
//   - Writes are idempotent, so retries on next state change are safe.
//   - Seq is only advanced after a successful write (rollback on failure).
package persist

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// A/B file suffixes.
const (
	suffixA = "_a.pb"
	suffixB = "_b.pb"
)

var crc32c = crc32.MakeTable(crc32.Castagnoli)

const (
	crcSize    = 4                 // CRC32-C prefix length
	seqSize    = 8                 // uint64 little-endian
	headerSize = crcSize + seqSize // file header: [4-byte CRC32-C BE][8-byte seq LE]
)

// ErrNoData is returned by loadPersisted when no persisted files exist for the prefix.
var ErrNoData = errors.New("no persisted data")

// ErrCorrupt indicates that a persisted file exists but contains invalid data
// (e.g. partially written during a crash). loadPersisted tolerates one corrupt
// file and falls back to the other A/B copy. OS-level errors (permission denied,
// I/O errors) are NOT wrapped with ErrCorrupt and cause loadPersisted to fail.
var ErrCorrupt = errors.New("corrupt persisted data")

// dataWithSeq is the unit stored in each A/B file: a sequence number and a proto payload.
type dataWithSeq struct {
	seq  uint64
	data []byte // nil on fresh start
}

// Persister[T] is a strongly-typed persister for a proto message type.
type Persister[T protoutils.Message] interface {
	Persist(T) error
}

type noopPersister[T protoutils.Message] struct{}

func (noopPersister[T]) Persist(T) error { return nil }

// newNoOpPersister returns a Persister that silently discards all writes.
func newNoOpPersister[T protoutils.Message]() Persister[T] {
	return noopPersister[T]{}
}

// abPersister writes data to A/B files with automatic seq management.
// File format: [4-byte CRC32-C BE] [8-byte seq LE] [proto-marshalled message].
// Only created when config has a state dir; dir is always a valid path.
// File selection is derived from seq: odd seq → A, even seq → B.
type abPersister[T protoutils.Message] struct {
	dir    string
	prefix string
	seq    uint64
}

// NewPersister creates a crash-safe persister for the given directory and prefix.
// When dir is None, returns a no-op persister that discards all writes.
// When dir is Some, it must already exist and be a directory; returns error otherwise.
// Also returns the loaded message (None on fresh start or no-op) for the caller to use.
// This encapsulates all on-disk format details (A/B files, seq wrapper, proto marshal) in one place.
func NewPersister[T protoutils.Message](dir utils.Option[string], prefix string) (Persister[T], utils.Option[T], error) {
	none := utils.None[T]()

	d, ok := dir.Get()
	if !ok {
		return newNoOpPersister[T](), none, nil
	}

	fi, err := os.Stat(d)
	if err != nil {
		return nil, none, fmt.Errorf("invalid state dir %q: %w", d, err)
	}
	if !fi.IsDir() {
		return nil, none, fmt.Errorf("invalid state dir %q: not a directory", d)
	}
	// Probe writability by creating and removing a temp file. Checking permission
	// bits is not reliable: Windows emulates Unix bits from ACLs, root bypasses
	// permission checks on Unix, and group/other write bits are easy to miss.
	probe, err := os.CreateTemp(d, ".writable-probe-*")
	if err != nil {
		return nil, none, fmt.Errorf("state dir %q is not writable: %w", d, err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())

	ds, err := loadPersisted(d, prefix)
	if err != nil && !errors.Is(err, ErrNoData) {
		return nil, none, err
	}

	// Ensure both A/B files exist and are writable so Persist never creates new
	// directory entries. Empty files are treated as non-existent by loadFile,
	// so they won't interfere with loading on restart.
	for _, suffix := range []string{suffixA, suffixB} {
		path := filepath.Join(d, prefix+suffix)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600) //nolint:gosec // path is stateDir + hardcoded suffix; not user-controlled
		if err != nil {
			return nil, none, fmt.Errorf("state file %q is not writable: %w", path, err)
		}
		_ = f.Close()
	}
	// Sync directory to make file entries durable (harmless if files already existed).
	if df, err := os.Open(d); err != nil { //nolint:gosec // d is operator-configured stateDir; not user-controlled
		return nil, none, fmt.Errorf("open directory %s: %w", d, err)
	} else if err := df.Sync(); err != nil {
		_ = df.Close()
		return nil, none, fmt.Errorf("sync directory %s: %w", d, err)
	} else {
		_ = df.Close()
	}

	var loaded utils.Option[T]
	if ds.data != nil {
		msg, err := protoutils.Unmarshal[T](ds.data)
		if err != nil {
			return nil, none, fmt.Errorf("unmarshal persisted %s: %w", prefix, err)
		}
		loaded = utils.Some(msg)
	}
	return &abPersister[T]{
		dir:    d,
		prefix: prefix,
		seq:    ds.seq,
	}, loaded, nil
}

// Persist writes a proto message to persistent storage. Not safe for concurrent use.
func (w *abPersister[T]) Persist(msg T) error {
	data := protoutils.Marshal(msg)
	seq := w.seq + 1

	// Odd seq → A, even seq → B.
	suffix := suffixB
	if seq%2 == 1 {
		suffix = suffixA
	}

	filename := w.prefix + suffix
	if err := writeFile(filepath.Join(w.dir, filename), dataWithSeq{seq: seq, data: data}); err != nil {
		return fmt.Errorf("persist to %s: %w", filename, err)
	}
	w.seq = seq
	return nil
}

// loadFile reads a single A/B file and returns its contents as a dataWithSeq.
// Returns os.ErrNotExist when the file does not exist.
// Returns ErrCorrupt on CRC mismatch or truncated header.
// OS-level errors (permission denied, I/O) are returned unwrapped.
func loadFile(stateDir, filename string) (dataWithSeq, error) {
	path := filepath.Join(stateDir, filename)
	bz, err := os.ReadFile(path) //nolint:gosec // path is constructed from operator-configured stateDir + hardcoded filename suffix; no user-controlled input
	if errors.Is(err, os.ErrNotExist) {
		return dataWithSeq{}, os.ErrNotExist
	}
	if err != nil {
		return dataWithSeq{}, fmt.Errorf("read %s: %w", filename, err)
	}
	// Empty files are created by NewPersister to pre-populate directory entries.
	if len(bz) == 0 {
		return dataWithSeq{}, os.ErrNotExist
	}
	if len(bz) < headerSize {
		return dataWithSeq{}, fmt.Errorf("%s: truncated (len %d < header %d): %w", filename, len(bz), headerSize, ErrCorrupt)
	}

	wantCRC := binary.BigEndian.Uint32(bz[:crcSize])
	payload := bz[crcSize:]
	if got := crc32.Checksum(payload, crc32c); got != wantCRC {
		return dataWithSeq{}, fmt.Errorf("%s: crc32 mismatch (got %08x, want %08x): %w", filename, got, wantCRC, ErrCorrupt)
	}

	seq := binary.LittleEndian.Uint64(payload[:seqSize])
	if seq == 0 {
		return dataWithSeq{}, fmt.Errorf("%s: zero seq: %w", filename, ErrCorrupt)
	}
	return dataWithSeq{seq: seq, data: payload[seqSize:]}, nil
}

// loadPersisted loads persisted data for the given directory and prefix.
// Tries both A and B files; if one is corrupt (e.g. crash during write), the other is used
// so the validator can restart. Returns ErrNoData when no persisted files exist (use errors.Is).
// Returns other error only when both files fail to load or state is inconsistent (same seq).
func loadPersisted(dir string, prefix string) (dataWithSeq, error) {
	fileA, fileB := prefix+suffixA, prefix+suffixB
	a, errA := loadFile(dir, fileA)
	b, errB := loadFile(dir, fileB)

	// Fail fast on OS-level errors (permission denied, I/O errors).
	// Only ErrNotExist (fresh start) and ErrCorrupt (crash mid-write) are tolerable.
	for _, fe := range []struct {
		file string
		err  error
	}{{fileA, errA}, {fileB, errB}} {
		if fe.err == nil {
			continue
		}
		if errors.Is(fe.err, os.ErrNotExist) {
			continue
		}
		if errors.Is(fe.err, ErrCorrupt) {
			logger.Warn("corrupt state file", "file", fe.file, "err", fe.err)
			continue
		}
		return dataWithSeq{}, fmt.Errorf("load %s: %w", fe.file, fe.err)
	}

	switch {
	case errA == nil && errB == nil:
		switch {
		case a.seq > b.seq:
			return a, nil
		case b.seq > a.seq:
			return b, nil
		default:
			return dataWithSeq{}, fmt.Errorf("corrupt state: both %s and %s have same seq; remove %s if acceptable", fileA, fileB, fileB)
		}
	case errA == nil:
		return a, nil
	case errB == nil:
		return b, nil
	default:
		if errors.Is(errA, os.ErrNotExist) && errors.Is(errB, os.ErrNotExist) {
			return dataWithSeq{}, ErrNoData
		}
		return dataWithSeq{}, fmt.Errorf("no valid state: %s: %v; %s: %v", fileA, errA, fileB, errB)
	}
}

// writeAndSync atomically replaces path contents with data (O_TRUNC) and fsyncs.
// Used by WAL persistence (blocks, commitqcs).
func writeAndSync(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600) //nolint:gosec // path is stateDir + hardcoded suffix; not user-controlled
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

// writeFile writes an A/B state file: [4-byte CRC32-C BE][8-byte seq LE][proto data].
// Encodes seq and computes CRC internally; writes chunks directly to avoid
// copying data into an intermediate buffer. The file is fsynced before return.
func writeFile(path string, d dataWithSeq) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600) //nolint:gosec // path is stateDir + hardcoded suffix; not user-controlled
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var seqBuf [seqSize]byte
	binary.LittleEndian.PutUint64(seqBuf[:], d.seq)

	// hash.Hash.Write never returns an error.
	h := crc32.New(crc32c)
	_, _ = h.Write(seqBuf[:])
	_, _ = h.Write(d.data)

	var crcBuf [crcSize]byte
	binary.BigEndian.PutUint32(crcBuf[:], h.Sum32())
	for _, chunk := range [][]byte{crcBuf[:], seqBuf[:], d.data} {
		if _, err := f.Write(chunk); err != nil {
			return err
		}
	}
	return f.Sync()
}
