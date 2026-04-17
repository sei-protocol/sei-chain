package migration

import (
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
type DBWriter func(changesets []*proto.NamedChangeSet) error

// Read a value from the database.
type DBReader func(store string, key []byte) ([]byte, bool, error)

const (
	// The store name for migration related metadata.
	MigrationStore = "migration"
	// The key where the migration boundary is stored for the memiavl -> flatkv migration.
	FlatKVMigrationBoundaryKey = "flatkv-migration-boundary"
)
