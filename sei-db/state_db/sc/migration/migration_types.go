package migration

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
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

// An object capable of routing/splitting reads and writes between databases.
type Router interface {
	// Read a value from the database.
	Read(store string, key []byte) ([]byte, bool, error)

	// Apply a batch of change sets to the database.
	//
	// Not atomic. Caller is respsible for providing crash safe atomicity.
	//
	// If this method returns an error, it is not safe to attempt to retry. An error should be considered
	// fatal, and should result in any managed databases being shut down and crash recovered.
	ApplyChangeSets(ctx context.Context, changesets []*proto.NamedChangeSet) error
}
