package wal

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// ChangelogWAL is a type alias for a WAL specialized for ChangelogEntry.
type ChangelogWAL = GenericWAL[proto.ChangelogEntry]

// NewChangelogWAL creates a new WAL for ChangelogEntry.
// This is a convenience wrapper that handles serialization automatically.
func NewChangelogWAL(logger logger.Logger, dir string, config Config) (ChangelogWAL, error) {
	return NewWAL(
		context.Background(),
		func(e proto.ChangelogEntry) ([]byte, error) { return e.Marshal() },
		func(data []byte) (proto.ChangelogEntry, error) {
			var e proto.ChangelogEntry
			err := e.Unmarshal(data)
			return e, err
		},
		logger,
		dir,
		config,
	)
}
