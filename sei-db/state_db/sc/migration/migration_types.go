package migration

import (
	"context"
	"fmt"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	dbm "github.com/tendermint/tm-db"
)

// MigrationStatus is the lifecycle status of a migration.
type MigrationStatus int

const (
	// MigrationNotStarted means the migration has not yet started. All keys are considered unmigrated.
	MigrationNotStarted MigrationStatus = 0
	// MigrationInProgress means the migration is in progress. Some keys are migrated, some are not.
	MigrationInProgress MigrationStatus = 1
	// MigrationComplete means the migration is complete. All keys are considered migrated.
	MigrationComplete MigrationStatus = 2
)

func (s MigrationStatus) String() string {
	switch s {
	case MigrationNotStarted:
		return "not_started"
	case MigrationInProgress:
		return "in_progress"
	case MigrationComplete:
		return "complete"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// Write a batch of values to the database.
//
// May not be atomic. If not atomic, then the caller must provide crash safe atomicity.
type DBWriter func(ctx context.Context, changesets []*proto.NamedChangeSet) error

// Read a value from the database.
type DBReader func(store string, key []byte) ([]byte, bool, error)

// Get an iterator over a range of keys in a store.
type DBIteratorBuilder func(store string, start []byte, end []byte, ascending bool) (dbm.Iterator, error)

// Builds a proof of the value for a key in a store.
type DBProofBuilder func(store string, key []byte) (*ics23.CommitmentProof, error)

// An object capable of routing/splitting reads and writes between databases.
type Router interface {
	// Read a value from the database.
	Read(store string, key []byte) ([]byte, bool, error)

	// Apply a batch of change sets to the database.
	//
	// Not atomic. Caller is responsible for providing crash safe atomicity.
	//
	// If this method returns an error, it is not safe to attempt to retry. An error should be considered
	// fatal, and should result in any managed databases being shut down and crash recovered.
	ApplyChangeSets(ctx context.Context, changesets []*proto.NamedChangeSet) error

	// Get an iterator over a range of keys in a store. Some stores may not support iteration,
	// and this method will return an error in that case.
	Iterator(store string, start []byte, end []byte, ascending bool) (dbm.Iterator, error)

	// Get a proof of the value for a key in a store. Some stores may not support proofs,
	// and this method will return an error in that case.
	GetProof(store string, key []byte) (*ics23.CommitmentProof, error)
}
