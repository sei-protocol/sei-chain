package disktable

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// GCWatermarkFileName is the name of the durable sidecar file that records the lowest readable segment index.
const GCWatermarkFileName = "gc-watermark"

// GCWatermarkFile is a small durable record of the lowest segment index that is still logically readable
// ("lowestReadableSegment"). Garbage collection advances it (and fsyncs this file) before it schedules a
// segment's keymap-entry deletion; reads, keymap repair, and keymap reload all refuse to touch segments below
// it. Segments below the watermark are logically deleted regardless of whether their keymap entries or files
// have physically been removed yet.
//
// The file lives at the table root (NOT inside the keymap directory) so that it survives a full keymap
// rebuild: a rebuild reconstructs the keymap from on-disk segment files and, by consulting this watermark,
// avoids resurrecting keys from segments that were being garbage collected when the keymap was lost.
//
// The on-disk format is a single human-readable integer (the lowest readable segment index). Writes are
// crash-safe (temp file + atomic rename) and fsynced: correctness depends on this watermark being durable no
// later than the keymap-entry deletes it guards (see gcManager.collectExpiredSegments). A swap file left
// behind by a crash mid-write is cleaned up at startup by util.DeleteOrphanedSwapFiles over the table root.
type GCWatermarkFile struct {
	// parentDirectory is the directory where the file is stored (the table root).
	parentDirectory string

	// lowestReadableSegment is the lowest segment index that is still logically readable.
	lowestReadableSegment uint32

	// defined is true once a value has been read from disk or written; false means the file does not yet exist.
	defined bool
}

// LoadGCWatermarkFile loads the gc-watermark file from parentDirectory. If the file does not exist, the
// returned file is undefined (IsDefined() == false) and can still be used to create the file via Update().
func LoadGCWatermarkFile(parentDirectory string) (*GCWatermarkFile, error) {
	watermark := &GCWatermarkFile{parentDirectory: parentDirectory}

	exists, err := util.Exists(watermark.Path())
	if err != nil {
		return nil, fmt.Errorf("failed to check if gc-watermark file %s exists: %w", watermark.Path(), err)
	}
	if !exists {
		return watermark, nil
	}

	data, err := os.ReadFile(watermark.Path())
	if err != nil {
		return nil, fmt.Errorf("failed to read gc-watermark file %s: %w", watermark.Path(), err)
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse gc-watermark file %s: %w", watermark.Path(), err)
	}
	watermark.lowestReadableSegment = uint32(value) //nolint:gosec // segment index fits uint32
	watermark.defined = true
	return watermark, nil
}

// Path returns the full path to the gc-watermark file.
func (w *GCWatermarkFile) Path() string {
	return path.Join(w.parentDirectory, GCWatermarkFileName)
}

// IsDefined returns true if the file exists on disk (or has been written this session).
func (w *GCWatermarkFile) IsDefined() bool {
	return w.defined
}

// LowestReadableSegment returns the durably-recorded lowest readable segment index. Only meaningful when
// IsDefined() is true.
func (w *GCWatermarkFile) LowestReadableSegment() uint32 {
	return w.lowestReadableSegment
}

// Update durably advances the watermark to lowestReadableSegment. The value is monotonic: lowering it is an
// error, and a no-op when unchanged. The write is crash-safe (temp file + atomic rename) and fsynced before
// returning, so once Update returns the new value is guaranteed durable.
func (w *GCWatermarkFile) Update(lowestReadableSegment uint32) error {
	if w.defined && lowestReadableSegment < w.lowestReadableSegment {
		return fmt.Errorf("gc-watermark may only increase, cannot set to %d (current: %d)",
			lowestReadableSegment, w.lowestReadableSegment)
	}
	if w.defined && lowestReadableSegment == w.lowestReadableSegment {
		return nil
	}

	data := []byte(fmt.Sprintf("%d\n", lowestReadableSegment))
	// fsync is required: a crash that loses this write while the keymap-entry deletes it guards are durable
	// would let keymap repair resurrect garbage-collected keys.
	if err := util.AtomicWrite(w.Path(), data, true); err != nil {
		return fmt.Errorf("failed to write gc-watermark file %s: %w", w.Path(), err)
	}
	w.lowestReadableSegment = lowestReadableSegment
	w.defined = true
	return nil
}
