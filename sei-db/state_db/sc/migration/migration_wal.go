package migration

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
)

// On-disk layout:
//
//	<dir>/
//	  <20-digit batchID>.rec       - a durable record.
//	  <20-digit batchID>.rec.tmp   - an in-flight write; ignored on Open.
//
// Each .rec file is self-contained, with the layout:
//
//	offset  size  field
//	0       4     checksum (CRC32-IEEE) of payload, big-endian
//	4       N     payload (opaque bytes)
//
// The active record is written to a .tmp file, fsync'd, atomically renamed
// to the final name, then the directory is fsync'd. That sequence is what
// makes a visible .rec file safe to trust: no torn-record state ever shows
// up under the final name.
//
// Only the record with the highest batchID is ever meaningful. Any older
// record is garbage as soon as a newer one lands, and is removed
// opportunistically by Append and defensively by Open.
const (
	walRecSuffix      = ".rec"
	walTempSuffix     = ".tmp"
	walRecHeaderSize  = 4
	walMaxPayloadSize = unit.GB // sanity cap
)

// MigrationWAL is a single-record write-ahead log used by MigrationManager
// to make cross-database writes atomic. It records one batch per successful
// Append and exposes the most recent durable batch via Latest so the caller
// can resume after a crash.
//
// There is nothing to close: each Append opens, fsyncs, and closes its own
// files. A MigrationWAL instance owns no OS resources between calls, so the
// caller can simply drop its reference when finished. Post-migration
// cleanup of the on-disk directory is the caller's responsibility and is
// not handled by this type.
//
// MigrationWAL is NOT safe for concurrent use; callers must serialize
// Append/Latest externally.
type MigrationWAL struct {
	// dir is the directory holding the .rec and .rec.tmp files. Created on
	// Open if it does not already exist.
	dir string
}

// OpenMigrationWAL opens (and if necessary creates) a MigrationWAL rooted at
// dir.
//
// Stale .tmp files from interrupted writes are removed. If more than one
// .rec file is present (because a crash landed between a successful write
// and the opportunistic cleanup of its predecessor), every file except the
// numerically-largest is unlinked.
func OpenMigrationWAL(dir string) (*MigrationWAL, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create WAL dir %q: %w", dir, err)
	}
	w := &MigrationWAL{dir: dir}
	if err := w.cleanupDir(); err != nil {
		return nil, err
	}
	return w, nil
}

// Append durably records a new entry. batchID must be exactly the latest
// batch ID plus one, or exactly 1 when the WAL is empty; otherwise an error
// is returned and no state changes.
//
// Returns only after the record is fsync'd and the directory entry is
// fsync'd. After the new record is durable, older records are best-effort
// unlinked; any stragglers are cleaned up on the next Open.
func (w *MigrationWAL) Append(batchID uint64, payload []byte) error {
	if len(payload) > walMaxPayloadSize {
		return fmt.Errorf("payload size %d exceeds max %d", len(payload), walMaxPayloadSize)
	}

	priorIDs, err := w.listRecordIDs()
	if err != nil {
		return fmt.Errorf("failed to list record IDs: %w", err)
	}
	var priorMax uint64
	if len(priorIDs) > 0 {
		priorMax = priorIDs[len(priorIDs)-1]
	}
	expected := priorMax + 1
	if batchID != expected {
		return fmt.Errorf("next batchID must be %d, got %d", expected, batchID)
	}

	finalName := formatRecName(batchID)
	tmpName := finalName + walTempSuffix
	finalPath := filepath.Join(w.dir, finalName)
	tmpPath := filepath.Join(w.dir, tmpName)

	if err := writeRecordFile(tmpPath, payload); err != nil {
		return fmt.Errorf("failed to write record file: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename WAL tmp to final: %w", err)
	}
	if err := syncDir(w.dir); err != nil {
		return fmt.Errorf("failed to fsync WAL dir: %w", err)
	}

	for _, id := range priorIDs {
		if err := os.Remove(filepath.Join(w.dir, formatRecName(id))); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Warn("failed to remove prior WAL record; will retry on next Open", "id", id, "err", err)
		}
	}
	return nil
}

// Latest returns the most recent durable record and its batch ID. Returns
// (0, nil, nil) if the WAL is empty.
func (w *MigrationWAL) Latest() (uint64, []byte, error) {
	ids, err := w.listRecordIDs()
	if err != nil {
		return 0, nil, fmt.Errorf("failed to list record IDs: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil, nil
	}
	id := ids[len(ids)-1]
	payload, err := w.readRecord(id)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to read record: %w", err)
	}
	return id, payload, nil
}

// cleanupDir enforces the at-most-one-record invariant. Removes any .tmp
// files and any .rec files other than the numerically-largest.
func (w *MigrationWAL) cleanupDir() error {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return fmt.Errorf("failed to read WAL dir: %w", err)
	}
	recIDs := make([]uint64, 0, len(entries))
	tmpFiles := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasSuffix(name, walTempSuffix):
			tmpFiles = append(tmpFiles, name)
		case strings.HasSuffix(name, walRecSuffix):
			id, ok := parseRecName(name)
			if ok {
				recIDs = append(recIDs, id)
			}
		}
	}

	toRemove := tmpFiles
	if len(recIDs) > 1 {
		sort.Slice(recIDs, func(i, j int) bool { return recIDs[i] < recIDs[j] })
		for _, id := range recIDs[:len(recIDs)-1] {
			toRemove = append(toRemove, formatRecName(id))
		}
	}

	for _, name := range toRemove {
		if err := os.Remove(filepath.Join(w.dir, name)); err != nil {
			return fmt.Errorf("failed to remove stale WAL file %q: %w", name, err)
		}
	}
	return nil
}

// listRecordIDs returns the batch IDs of all valid .rec files in ascending
// order. Unrecognized files are ignored.
func (w *MigrationWAL) listRecordIDs() ([]uint64, error) {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read WAL dir: %w", err)
	}
	ids := make([]uint64, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, walTempSuffix) || !strings.HasSuffix(name, walRecSuffix) {
			continue
		}
		id, ok := parseRecName(name)
		if !ok {
			continue
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

// readRecord loads the payload for a single record and verifies its
// checksum. A mismatch is a hard error; callers should treat it as
// corruption.
func (w *MigrationWAL) readRecord(id uint64) ([]byte, error) {
	path := filepath.Join(w.dir, formatRecName(id))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read WAL record %q: %w", path, err)
	}
	if len(data) < walRecHeaderSize {
		return nil, fmt.Errorf("WAL record %q is truncated: %d bytes", path, len(data))
	}
	expectedChecksum := binary.BigEndian.Uint32(data[0:walRecHeaderSize])
	payload := data[walRecHeaderSize:]
	if crc32.ChecksumIEEE(payload) != expectedChecksum {
		return nil, fmt.Errorf("WAL record %q failed checksum check", path)
	}
	return payload, nil
}

// writeRecordFile writes [checksum][payload] to path, fsyncs the file, and
// closes it. On any error the partially-written file is removed.
func writeRecordFile(path string, payload []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("failed to create WAL tmp: %w", err)
	}
	abort := func() {
		_ = f.Close()
		_ = os.Remove(path)
	}

	var header [walRecHeaderSize]byte
	binary.BigEndian.PutUint32(header[0:walRecHeaderSize], crc32.ChecksumIEEE(payload))
	if _, err := f.Write(header[:]); err != nil {
		abort()
		return fmt.Errorf("failed to write WAL header: %w", err)
	}
	if _, err := f.Write(payload); err != nil {
		abort()
		return fmt.Errorf("failed to write WAL payload: %w", err)
	}
	if err := f.Sync(); err != nil {
		abort()
		return fmt.Errorf("failed to fsync WAL tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("failed to close WAL tmp: %w", err)
	}
	return nil
}

// formatRecName produces the canonical .rec filename for a batch ID. The
// zero-padded width ensures lexicographic order matches numeric order, so
// sorted directory listings line up with batchID order.
func formatRecName(id uint64) string {
	return fmt.Sprintf("%020d%s", id, walRecSuffix)
}

// parseRecName extracts a batchID from a name like "00000000000000000042.rec".
func parseRecName(name string) (uint64, bool) {
	if !strings.HasSuffix(name, walRecSuffix) {
		return 0, false
	}
	id, err := strconv.ParseUint(strings.TrimSuffix(name, walRecSuffix), 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// syncDir fsyncs a directory so that file creates/renames/unlinks below it
// become durable.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
