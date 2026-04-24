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

// Write a batch of values to the database. Assumed to be atomic.
//
// The ctx is forwarded unchanged so a DBWriter may be another Router's
// ApplyChangeSets, allowing routers to be composed without adapter glue.
type DBWriter func(ctx context.Context, changesets []*proto.NamedChangeSet) error

// Read a value from the database.
type DBReader func(store string, key []byte) ([]byte, bool, error)

// An object capable of routing/splitting reands and writes between databases.
type Router interface {
	// Read a value from the database.
	Read(store string, key []byte) ([]byte, bool, error)

	// Apply a batch of change sets to the database.
	ApplyChangeSets(ctx context.Context, changesets []*proto.NamedChangeSet) error
}
