package migration

// A value to migrate.
type ValueToMigrate struct {
	// The module name of the value (part of the logical key).
	ModuleName string
	// The key of the value.
	Key []byte
	// The value to migrate.
	Value []byte
}

// An iterator for walking a DB in order to do a migration.
type MigrationIterator interface {

	// Returns the batch of values to be migrated.
	// Returns the new migration boundary to use after the batch is migrated.
	NextBatch(
		// The maximum size of the batch. Fewer may be returned if there are not enough values to fill the batch.
		// When zero values are returned, the migration is complete.
		size int,
	) ([]ValueToMigrate, MigrationBoundary, error)
}
