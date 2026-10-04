package wal

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
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
	for i, name := range names {
		src, dst := filepath.Join(dir, name), filepath.Join(view, name)
		// Opening a log truncates a corrupted tail and renames truncation
		// markers in place, so files it may modify are copied, not linked.
		if i == len(names)-1 || isTruncationMarker(name) {
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
