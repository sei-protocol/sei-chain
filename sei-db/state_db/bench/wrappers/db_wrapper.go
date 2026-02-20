package wrappers

import "github.com/sei-protocol/sei-chain/sei-db/proto"

// This benchmarking utility is capable of benchmarking a DB that implements this interface.
type DBWrapper interface {
	// ApplyChangeSets buffers EVM changesets (x/evm memiavl keys) and updates LtHash.
	// Non-EVM modules are ignored. Call Commit to persist.
	ApplyChangeSets(cs []*proto.NamedChangeSet) error

	// Commit persists buffered writes and advances the version.
	Commit() (int64, error)

	// Close releases any resources held by the DB.
	Close() error

	// Returns the latest committed version.
	Version() int64
}
