package wal

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// readOnlyViewAttempts bounds how often building a read-only view is retried
// when the log's writer removes a segment while the view is being built.
const readOnlyViewAttempts = 8

var errReadOnly = errors.New("wal is read-only")

// createReadOnlyView returns a new directory beside dir holding a private view
// of the log in dir. The caller removes it once the log opened from it is
// closed.
func createReadOnlyView(dir string) (string, error) {
	dir = filepath.Clean(dir)
	var err error
	for attempt := 0; attempt < readOnlyViewAttempts; attempt++ {
		var view string
		view, err = os.MkdirTemp(filepath.Dir(dir), filepath.Base(dir)+"-readonly-*-tmp")
		if err != nil {
			return "", fmt.Errorf("create read-only view of %s: %w", dir, err)
		}
		if err = populateReadOnlyView(dir, view); err == nil {
			return view, nil
		}
		_ = os.RemoveAll(view)
		if !errors.Is(err, fs.ErrNotExist) {
			break
		}
	}
	return "", fmt.Errorf("create read-only view of %s: %w", dir, err)
}

// populateReadOnlyView fills view with the segment files of the log in dir, so
// that opening the log from view never modifies dir. It returns an error
// matching fs.ErrNotExist when the writer removed a file listed in dir before
// it could be added to view.
func populateReadOnlyView(dir, view string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var names []string
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			names = append(names, entry.Name())
		}
	}
	modifiable := modifiableOnOpen(names)
	for _, name := range names {
		src, dst := filepath.Join(dir, name), filepath.Join(view, name)
		// Files an open may modify in place are copied, not linked.
		if modifiable[name] {
			err = copyFile(src, dst)
		} else {
			err = os.Link(src, dst)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// modifiableOnOpen returns the files among names, in directory order, that
// opening the log may modify in place.
func modifiableOnOpen(names []string) map[string]bool {
	modifiable := make(map[string]bool)
	var tail, repairTarget string
	for _, name := range names {
		if len(name) < 20 {
			continue
		}
		// The tail repair in open truncates the last long name, which a stray
		// file can make differ from the tail segment the log loads.
		repairTarget = name
		if isSegment(name) {
			tail = name
		}
		if isTruncationMarker(name) {
			modifiable[name] = true
		}
	}
	for _, name := range []string{tail, repairTarget} {
		if name != "" {
			modifiable[name] = true
		}
	}
	return modifiable
}

// isSegment reports whether opening the log loads name as a segment.
func isSegment(name string) bool {
	if len(name) < 20 {
		return false
	}
	index, err := strconv.ParseUint(name[:20], 10, 64)
	if err != nil || index == 0 {
		return false
	}
	return len(name) == 20 ||
		(len(name) == 26 && strings.HasSuffix(name, ".START")) ||
		(len(name) == 24 && strings.HasSuffix(name, ".END"))
}

// isTruncationMarker reports whether name is the marker file an interrupted
// front or back truncation leaves in the log directory.
func isTruncationMarker(name string) bool {
	return strings.HasSuffix(name, ".START") || strings.HasSuffix(name, ".END")
}

// copyFile copies the regular file src to the new file dst.
func copyFile(src, dst string) error {
	in, err := os.Open(filepath.Clean(src))
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(filepath.Clean(dst), os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
