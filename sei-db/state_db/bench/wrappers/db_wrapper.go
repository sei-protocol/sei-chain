package wrappers

import (
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// This benchmarking utility is capable of benchmarking a DB that implements this interface.
type DBWrapper interface {
	// ApplyChangeSets buffers EVM changesets (x/evm memiavl keys) and updates LtHash.
	// Non-EVM modules are ignored. Call Commit to persist.
	ApplyChangeSets(cs []*proto.NamedChangeSet) error

	// Read reads the value for the given key.
	Read(key []byte) (data []byte, found bool, err error)

	// Commit persists buffered writes and advances the version.
	Commit() (int64, error)

	// Close releases any resources held by the DB.
	Close() error

	// Version returns the latest committed version.
	Version() int64

	LoadVersion(version int64) error

	// Importer return an importer which load snapshot data into the database
	Importer(version int64) (types.Importer, error)
}
