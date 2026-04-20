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

	// MigrationVersionKey stores the current migration version as an
	// 8-byte BigEndian uint64. An absent key is interpreted as version 0.
	// This key is the source of truth for "which migration, if any, is
	// active" and survives across process restarts.
	//
	// Ownership: MigrationManager writes this key to the new database
	// exactly once per migration lifecycle, on the first ApplyChangeSets
	// call after the migration boundary reaches Complete ("bump block").
	// The same write atomically deletes MigrationBoundaryKey and
	// NewDBBatchIDKey from the new database.
	//
	// During a chained migration the key is typically already present in
	// the old database at the prior migration's destVersion
	// (= this migration's startVersion); the new database's copy, when
	// written at bump time, shadows it via the new-DB-first lookup used
	// in the constructor.
	MigrationVersionKey = "migration-version"
)
