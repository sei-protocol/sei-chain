package wal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tidwall/wal"
)

// ErrCorrupt reports a changelog that cannot be read without repair.
var ErrCorrupt = wal.ErrCorrupt

// segmentNameLen is the length of a changelog segment file name.
const segmentNameLen = 20

// VerifyIntact reports whether the changelog in dir can be opened without
// repair. It returns ErrCorrupt when the tail segment ends mid-record or an
// interrupted truncation is still in progress, and never modifies dir.
//
// A reader on a live node calls this before it opens the changelog, because the
// opener repairs what it finds: a torn tail is truncated, and an interrupted
// truncation is completed by renaming or removing segments. On a live node a
// torn tail is usually a write in progress rather than lasting damage, so the
// caller reruns instead of repairing.
func VerifyIntact(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read changelog dir %s: %w", dir, err)
	}

	// os.ReadDir sorts by name, and segment names are zero-padded, so the last
	// match is the tail segment the opener would truncate.
	var tail string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || len(name) < segmentNameLen {
			continue
		}
		if strings.HasSuffix(name, ".START") || strings.HasSuffix(name, ".END") {
			return fmt.Errorf("%w: changelog truncation marker %s is present in %s", ErrCorrupt, name, dir)
		}
		tail = name
	}
	if tail == "" {
		return nil
	}

	path := filepath.Join(dir, tail)
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("read changelog segment %s: %w", path, err)
	}
	for pos := 0; pos < len(data); {
		n, err := loadNextBinaryEntry(data[pos:])
		if err != nil {
			return fmt.Errorf("%w: changelog segment %s ends mid-record at offset %d", ErrCorrupt, path, pos)
		}
		pos += n
	}
	return nil
}
