package wal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	tidwallwal "github.com/tidwall/wal"
)

var (
	// ErrReadOnly is returned when a caller tries to mutate a read-only WAL.
	ErrReadOnly = errors.New("WAL is read-only")

	// ErrCorrupt identifies a malformed or unstable WAL view. It aliases the
	// underlying tidwall sentinel so callers do not need to import the storage
	// implementation only to decide whether a read-only open can be retried.
	ErrCorrupt = tidwallwal.ErrCorrupt
)

type readOnlySegment struct {
	name  string
	index uint64
}

type readOnlyEntry struct {
	file       *os.File
	dataOffset int64
	size       int
}

// readOnlyWAL is an immutable view of the plain segment files present when it
// opens. It does not use tidwall/wal.Open because that function creates files,
// opens the tail for writing, and completes interrupted truncations by removing
// and renaming segment files.
type readOnlyWAL[T any] struct {
	unmarshal   UnmarshalFn[T]
	files       []*os.File
	entries     []readOnlyEntry
	firstOffset uint64
	closed      atomic.Bool
	closeOnce   sync.Once
	closeErr    error
}

func openReadOnlyWAL[T any](dir string, unmarshal UnmarshalFn[T]) (*readOnlyWAL[T], error) {
	segments, err := listReadOnlySegments(dir)
	if err != nil {
		return nil, err
	}

	log := &readOnlyWAL[T]{unmarshal: unmarshal}
	cleanup := func(err error) (*readOnlyWAL[T], error) {
		_ = log.Close()
		return nil, err
	}

	var nextIndex uint64
	for i, segment := range segments {
		if i > 0 && segment.index != nextIndex {
			return cleanup(fmt.Errorf("%w: segment %s starts at index %d, expected %d",
				tidwallwal.ErrCorrupt, segment.name, segment.index, nextIndex))
		}

		path := filepath.Join(dir, segment.name)
		file, err := os.Open(filepath.Clean(path))
		if err != nil {
			return cleanup(fmt.Errorf("open WAL segment %s: %w", path, err))
		}
		log.files = append(log.files, file)

		info, err := file.Stat()
		if err != nil {
			return cleanup(fmt.Errorf("stat WAL segment %s: %w", path, err))
		}
		if !info.Mode().IsRegular() {
			return cleanup(fmt.Errorf("%w: WAL segment %s is not a regular file", tidwallwal.ErrCorrupt, path))
		}

		data, err := io.ReadAll(io.NewSectionReader(file, 0, info.Size()))
		if err != nil {
			return cleanup(fmt.Errorf("read WAL segment %s: %w", path, err))
		}
		if int64(len(data)) != info.Size() {
			return cleanup(fmt.Errorf("%w: WAL segment %s changed while it was read",
				tidwallwal.ErrCorrupt, path))
		}

		entries, err := indexReadOnlySegment(file, data)
		if err != nil {
			return cleanup(fmt.Errorf("index WAL segment %s: %w", path, err))
		}
		if len(entries) == 0 && i != len(segments)-1 {
			return cleanup(fmt.Errorf("%w: non-tail WAL segment %s is empty", tidwallwal.ErrCorrupt, path))
		}
		if len(log.entries) == 0 && len(entries) > 0 {
			log.firstOffset = segment.index
		}
		log.entries = append(log.entries, entries...)
		nextIndex = segment.index + uint64(len(entries))
	}
	return log, nil
}

func listReadOnlySegments(dir string) ([]readOnlySegment, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read WAL directory %s: %w", dir, err)
	}

	segments := make([]readOnlySegment, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".START") || strings.HasSuffix(name, ".END") {
			return nil, fmt.Errorf("%w: WAL recovery marker %s is present; retry after the writer finishes",
				tidwallwal.ErrCorrupt, name)
		}
		if len(name) != 20 {
			continue
		}
		index, err := strconv.ParseUint(name, 10, 64)
		if err != nil || index == 0 {
			continue
		}
		segments = append(segments, readOnlySegment{name: name, index: index})
	}
	sort.Slice(segments, func(i, j int) bool {
		return segments[i].index < segments[j].index
	})
	return segments, nil
}

func indexReadOnlySegment(file *os.File, data []byte) ([]readOnlyEntry, error) {
	entries := make([]readOnlyEntry, 0)
	for pos := 0; pos < len(data); {
		size, prefixLen := binary.Uvarint(data[pos:])
		if prefixLen <= 0 || size > math.MaxInt32 {
			return nil, tidwallwal.ErrCorrupt
		}
		if len(data)-pos-prefixLen < int(size) {
			return nil, tidwallwal.ErrCorrupt
		}
		entries = append(entries, readOnlyEntry{
			file:       file,
			dataOffset: int64(pos + prefixLen),
			size:       int(size),
		})
		pos += prefixLen + int(size)
	}
	return entries, nil
}

func (log *readOnlyWAL[T]) Write(T) error {
	return ErrReadOnly
}

func (log *readOnlyWAL[T]) TruncateBefore(uint64) error {
	return ErrReadOnly
}

func (log *readOnlyWAL[T]) TruncateAfter(uint64) error {
	return ErrReadOnly
}

func (log *readOnlyWAL[T]) TruncateAll() error {
	return ErrReadOnly
}

func (log *readOnlyWAL[T]) FirstOffset() (uint64, error) {
	if log.closed.Load() {
		return 0, os.ErrClosed
	}
	if len(log.entries) == 0 {
		return 0, nil
	}
	return log.firstOffset, nil
}

func (log *readOnlyWAL[T]) LastOffset() (uint64, error) {
	if log.closed.Load() {
		return 0, os.ErrClosed
	}
	if len(log.entries) == 0 {
		return 0, nil
	}
	return log.firstOffset + uint64(len(log.entries)) - 1, nil
}

func (log *readOnlyWAL[T]) ReadAt(index uint64) (T, error) {
	var zero T
	if log.closed.Load() {
		return zero, os.ErrClosed
	}
	if index < log.firstOffset || index-log.firstOffset >= uint64(len(log.entries)) {
		return zero, fmt.Errorf("read WAL offset %d: out of range", index)
	}

	entry := log.entries[index-log.firstOffset]
	data := make([]byte, entry.size)
	if _, err := entry.file.ReadAt(data, entry.dataOffset); err != nil {
		return zero, fmt.Errorf("read WAL offset %d: %w", index, err)
	}
	value, err := log.unmarshal(data)
	if err != nil {
		return zero, fmt.Errorf("unmarshal WAL offset %d: %w", index, err)
	}
	return value, nil
}

func (log *readOnlyWAL[T]) Replay(start, end uint64, processFn func(index uint64, entry T) error) error {
	if end < start {
		return nil
	}
	for index := start; index <= end; index++ {
		entry, err := log.ReadAt(index)
		if err != nil {
			return err
		}
		if err := processFn(index, entry); err != nil {
			return fmt.Errorf("process WAL offset %d: %w", index, err)
		}
	}
	return nil
}

func (log *readOnlyWAL[T]) Close() error {
	log.closeOnce.Do(func() {
		log.closed.Store(true)
		var errs []error
		for _, file := range log.files {
			if err := file.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		log.closeErr = errors.Join(errs...)
	})
	return log.closeErr
}
