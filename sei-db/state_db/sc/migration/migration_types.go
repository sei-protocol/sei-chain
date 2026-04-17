package migration

import "github.com/sei-protocol/sei-chain/sei-db/proto"

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

// Write a batch of values to the database.
type DBWriter func(changesets []*proto.NamedChangeSet) error

// Read a value from the database.
type DBReader func(store string, key []byte) ([]byte, error)
