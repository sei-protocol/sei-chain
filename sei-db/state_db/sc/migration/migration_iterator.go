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

// MigrationIterator walks a DB in lexicographic (moduleName, key) order,
// yielding batches of values to be copied to a new DB.
//
// The underlying data may be mutated between NextBatch calls. Writes to keys
// that are at or before the current boundary (i.e. already migrated) are
// invisible to future NextBatch calls. Writes to keys after the boundary will
// be observed when the iterator reaches them.
type MigrationIterator interface {

	// NextBatch returns the next batch of values to be migrated and the new
	// migration boundary after the batch. Fewer than size values may be
	// returned if there are not enough remaining. When zero values are
	// returned, the migration is complete.
	NextBatch(size int) ([]ValueToMigrate, MigrationBoundary, error)
}
