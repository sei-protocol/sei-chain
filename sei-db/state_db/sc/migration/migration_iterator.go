package migration

// A value to migrate.
type ValueToMigrate struct {
	// The module name of the value (part of the logical key).
	ModuleName string
	// The key of the value. Must not be mutated by the caller.
	Key []byte
	// The value to migrate. Must not be mutated by the caller.
	Value []byte
}

// MigrationIterator walks a DB in lexicographic (moduleName, key) order,
// yielding batches of values to be copied to a new DB.
//
// The underlying data may be mutated between NextBatch calls. Writes to keys
// that are at or before the current boundary (i.e. already migrated) are
// invisible to future NextBatch calls. Writes to keys after the boundary will
// be observed when the iterator reaches them.
//
// Implementations MUST skip the reserved MigrationStore module. The keys
// under it (MigrationBoundaryKey, MigrationVersionKey, OldDBBatchIDKey,
// NewDBBatchIDKey) are migration metadata owned by MigrationManager.
// Yielding them would cause the manager to copy a stale startVersion over
// the destVersion it writes at bump time, and in general to clobber its
// own bookkeeping on the destination side.
type MigrationIterator interface {

	// SetBoundary repositions the iterator so that subsequent NextBatch calls
	// resume just past the given boundary. This may be called at any time
	// between NextBatch calls.
	SetBoundary(boundary MigrationBoundary)

	// NextBatch returns the next batch of values to be migrated and the new
	// migration boundary after the batch. Fewer than size values may be
	// returned if there are not enough remaining. When zero values are
	// returned, the migration is complete.
	//
	// size must be > 0; otherwise an error is returned and iterator state
	// is unchanged.
	NextBatch(size int) ([]ValueToMigrate, MigrationBoundary, error)
}
