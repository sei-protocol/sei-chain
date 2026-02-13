// Crash-safe A/B file persistence.
//
// # stateDir Configuration
//
// At config level, stateDir is an Option[string]. NewState creates a persister when
// stateDir is Some(path). When None, no persister is created (persistence
// disabled — DANGEROUS, may lead to SLASHING if the node restarts and double-votes;
// only use for testing). When Some(path), the path must already exist and be
// writable (verified by writing a temp file at startup); returns error otherwise.
// TODO: surface the None warning in CLI --help (e.g. stream command or config docs).
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
package consensus

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/proto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const (
	suffixA = "_a.pb"
	suffixB = "_b.pb"
)

// ErrNoData is returned by loadPersisted when no persisted files exist for the prefix.
var ErrNoData = errors.New("no persisted data")

// ErrCorrupt indicates that a persisted file exists but contains invalid data
// (e.g. partially written during a crash). loadPersisted tolerates one corrupt
// file and falls back to the other A/B copy. OS-level errors (permission denied,
// I/O errors) are NOT wrapped with ErrCorrupt and cause loadPersisted to fail.
var ErrCorrupt = errors.New("corrupt persisted data")

// Persister writes data to persistent storage. Persist returns error on failure.
type Persister interface {
	Persist(data []byte) error
}

// persister writes data to A/B files with automatic seq management.
// Uses PersistedWrapper protobuf for crash-safe persistence.
// Only created when config has a state dir; dir is always a valid path.
// File selection is derived from seq: odd seq → A, even seq → B.
type persister struct {
	dir    string
	prefix string
	seq    uint64
}

// newPersister creates a crash-safe persister for the given directory and prefix.
// dir must already exist and be a directory (we do not create it); returns error otherwise.
// Also returns the loaded data (None on fresh start) for the caller to pass to newInner.
// This encapsulates all on-disk format details (A/B files, seq wrapper) in one place.
func newPersister(dir string, prefix string) (*persister, utils.Option[[]byte], error) {
	none := utils.None[[]byte]()

	fi, err := os.Stat(dir)
	if err != nil {
		return nil, none, fmt.Errorf("invalid state dir %q: %w", dir, err)
	}
	if !fi.IsDir() {
		return nil, none, fmt.Errorf("invalid state dir %q: not a directory", dir)
	}
	// Probe writability by creating and removing a temp file. Checking permission
	// bits is not reliable: Windows emulates Unix bits from ACLs, root bypasses
	// permission checks on Unix, and group/other write bits are easy to miss.
	probe, err := os.CreateTemp(dir, ".writable-probe-*")
	if err != nil {
		return nil, none, fmt.Errorf("state dir %q is not writable: %w", dir, err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())

	wrapper, err := loadPersisted(dir, prefix)
	if err != nil && !errors.Is(err, ErrNoData) {
		return nil, none, err
	}

	// Ensure both A/B files exist and are writable so Persist never creates new
	// directory entries. Empty files are treated as non-existent by loadWrapped,
	// so they won't interfere with loading on restart.
	for _, suffix := range []string{suffixA, suffixB} {
		path := filepath.Join(dir, prefix+suffix)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600) //nolint:gosec // path is stateDir + hardcoded suffix; not user-controlled
		if err != nil {
			return nil, none, fmt.Errorf("state file %q is not writable: %w", path, err)
		}
		_ = f.Close()
	}
	// Sync directory to make file entries durable (harmless if files already existed).
	if d, err := os.Open(dir); err != nil { //nolint:gosec // dir is operator-configured stateDir; not user-controlled
		return nil, none, fmt.Errorf("open directory %s: %w", dir, err)
	} else if err := d.Sync(); err != nil {
		_ = d.Close()
		return nil, none, fmt.Errorf("sync directory %s: %w", dir, err)
	} else {
		_ = d.Close()
	}

	// wrapper is nil on fresh start (ErrNoData); protobuf Get methods return zero values for nil.
	var data utils.Option[[]byte]
	if d := wrapper.GetData(); d != nil {
		data = utils.Some(d)
	}
	return &persister{
		dir:    dir,
		prefix: prefix,
		seq:    wrapper.GetSeq(),
	}, data, nil
}

// Persist writes data to persistent storage with seq wrapper.
// Not safe for concurrent use.
// Returns error on marshal or write failure.
func (w *persister) Persist(data []byte) error {
	seq := w.seq + 1

	// Odd seq → A, even seq → B.
	suffix := suffixB
	if seq%2 == 1 {
		suffix = suffixA
	}
	filename := w.prefix + suffix

	wrapper := &pb.PersistedWrapper{
		Seq:  &seq,
		Data: data,
	}
	bz, err := proto.Marshal(wrapper)
	if err != nil {
		return fmt.Errorf("marshal wrapper: %w", err)
	}

	if err := writeAndSync(w.dir, filename, bz); err != nil {
		return fmt.Errorf("persist to %s: %w", filename, err)
	}
	w.seq = seq
	return nil
}

// loadWrapped loads a wrapped file, returning the PersistedWrapper proto.
// Returns os.ErrNotExist when the file does not exist (caller can use errors.Is).
// Returns other error on read or unmarshal failure. loadPersisted calls loadWrapped
// for both A and B and only fails when both fail; one corrupt file is tolerated.
// stateDir must be an existing directory (we do not create it).
func loadWrapped(stateDir, filename string) (*pb.PersistedWrapper, error) {
	path := filepath.Join(stateDir, filename)
	bz, err := os.ReadFile(path) //nolint:gosec // path is constructed from operator-configured stateDir + hardcoded filename suffix; no user-controlled input
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		// OS-level read error (permission denied, I/O error, etc.) —
		// not wrapped with ErrCorrupt so loadPersisted propagates it.
		return nil, fmt.Errorf("read %s: %w", filename, err)
	}
	// Treat empty files as non-existent. A valid wrapper must contain at least
	// a seq number. Empty files are created by newPersister to pre-populate
	// directory entries so that Persist never needs to dir-sync.
	if len(bz) == 0 {
		return nil, os.ErrNotExist
	}
	var wrapper pb.PersistedWrapper
	if err := proto.Unmarshal(bz, &wrapper); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", filename, fmt.Errorf("%v: %w", err, ErrCorrupt))
	}
	return &wrapper, nil
}

// loadPersisted loads persisted data for the given directory and prefix.
// Tries both A and B files; if one is corrupt (e.g. crash during write), the other is used
// so the validator can restart. Returns ErrNoData when no persisted files exist (use errors.Is).
// Returns other error only when both files fail to load or state is inconsistent (same seq).
func loadPersisted(dir string, prefix string) (*pb.PersistedWrapper, error) {
	fileA, fileB := prefix+suffixA, prefix+suffixB
	wrapperA, errA := loadWrapped(dir, fileA)
	wrapperB, errB := loadWrapped(dir, fileB)

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
			log.Warn().Str("file", fe.file).Err(fe.err).Msg("corrupt state file")
			continue
		}
		return nil, fmt.Errorf("load %s: %w", fe.file, fe.err)
	}

	switch {
	case errA == nil && errB == nil:
		switch {
		case wrapperA.GetSeq() > wrapperB.GetSeq():
			return wrapperA, nil
		case wrapperB.GetSeq() > wrapperA.GetSeq():
			return wrapperB, nil
		default:
			return nil, fmt.Errorf("corrupt state: both %s and %s have same seq; remove %s if acceptable", fileA, fileB, fileB)
		}
	case errA == nil:
		return wrapperA, nil
	case errB == nil:
		return wrapperB, nil
	default:
		if errors.Is(errA, os.ErrNotExist) && errors.Is(errB, os.ErrNotExist) {
			return nil, ErrNoData
		}
		return nil, fmt.Errorf("no valid state: %s: %v; %s: %v", fileA, errA, fileB, errB)
	}
}

// writeAndSync writes data to file and fsyncs. No dir sync needed because
// newPersister pre-creates both A/B files at startup.
func writeAndSync(stateDir string, filename string, data []byte) error {
	path := filepath.Join(stateDir, filename)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600) //nolint:gosec // path is stateDir + hardcoded suffix; not user-controlled
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
