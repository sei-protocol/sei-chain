package migration

// MigrationStore is the store name reserved for migration metadata.
// MigrationManager owns this key space: reads are always served from
// the new database, and external writes are rejected.
const MigrationStore = "migration"

const (
	// The key where the migration boundary is stored.
	//
	// This key always lives in the new database.
	MigrationBoundaryKey = "migration-boundary"

	// The key where the migration batch ID of the last write to the old database is stored.
	// Used to recover from the migration WAL.
	//
	// This key always lives in the old database.
	OldDBBatchIDKey = "old-db-batch-id"

	// The key where the batch ID of the last write to the new database is stored.
	//  Used to recover from the migration WAL.

	// This key always lives in the new database.
	NewDBBatchIDKey = "new-db-batch-id"
)
