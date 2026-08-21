package wal

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// ChangelogWAL is a type alias for a WAL specialized for ChangelogEntry.
type ChangelogWAL = GenericWAL[proto.ChangelogEntry]

// NewChangelogWAL creates a new WAL for ChangelogEntry.
// This is a convenience wrapper that handles serialization automatically.
func NewChangelogWAL(dir string, config Config) (ChangelogWAL, error) {
	return NewWAL(
		context.Background(),
		func(e proto.ChangelogEntry) ([]byte, error) { return e.Marshal() },
		func(data []byte) (proto.ChangelogEntry, error) {
			var e proto.ChangelogEntry
			err := e.Unmarshal(data)
			return e, err
		},
		dir,
		config,
	)
}

// OpenReadOnlyChangelogWAL opens an immutable point-in-time view of the
// changelog segment files. It never creates, truncates, removes, or renames WAL
// files. An incomplete tail, malformed record, or recovery marker returns
// ErrCorrupt.
func OpenReadOnlyChangelogWAL(dir string) (ChangelogWAL, error) {
	readOnly, err := openReadOnlyWAL(
		dir,
		func(data []byte) (proto.ChangelogEntry, error) {
			var entry proto.ChangelogEntry
			err := entry.Unmarshal(data)
			return entry, err
		},
	)
	if err != nil {
		return nil, err
	}
	return readOnly, nil
}

// FindFirstOffsetAfterVersion returns the first WAL offset whose entry version is
// strictly greater than targetVersion. If no such entry exists, it returns
// lastOffset+1. Changelog versions are monotonic, but empty blocks can advance
// the state-store version without writing an entry, so callers must search the
// entry versions rather than assuming offset == version.
func FindFirstOffsetAfterVersion(
	stream ChangelogWAL,
	firstOffset uint64,
	lastOffset uint64,
	targetVersion int64,
) (uint64, error) {
	lo, hi := firstOffset, lastOffset
	result := lastOffset + 1

	for lo <= hi {
		mid := lo + (hi-lo)/2
		entry, err := stream.ReadAt(mid)
		if err != nil {
			return 0, fmt.Errorf("read WAL at offset %d: %w", mid, err)
		}
		if entry.Version > targetVersion {
			result = mid
			if mid == firstOffset {
				break
			}
			hi = mid - 1
		} else {
			lo = mid + 1
		}
	}
	return result, nil
}

// FindLastOffsetAtOrBeforeVersion returns the last WAL offset whose entry
// version is less than or equal to targetVersion. If every entry is above the
// target, ok is false.
func FindLastOffsetAtOrBeforeVersion(
	stream ChangelogWAL,
	firstOffset uint64,
	lastOffset uint64,
	targetVersion int64,
) (offset uint64, ok bool, err error) {
	firstAfter, err := FindFirstOffsetAfterVersion(stream, firstOffset, lastOffset, targetVersion)
	if err != nil {
		return 0, false, err
	}
	if firstAfter == firstOffset {
		return 0, false, nil
	}
	return firstAfter - 1, true, nil
}
