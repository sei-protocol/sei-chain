package wal

import (
	"context"

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
// files. A torn tail or an in-progress tidwall recovery marker returns
// tidwall/wal.ErrCorrupt so callers can fail and retry after the writer moves
// on.
func OpenReadOnlyChangelogWAL(dir string) (ChangelogWAL, error) {
	return openReadOnlyWAL(
		dir,
		func(data []byte) (proto.ChangelogEntry, error) {
			var entry proto.ChangelogEntry
			err := entry.Unmarshal(data)
			return entry, err
		},
	)
}
