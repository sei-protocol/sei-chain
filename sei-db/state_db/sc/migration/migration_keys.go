package migration

// MigrationStore is the store name reserved for MigrationManager
// bookkeeping (MigrationVersionKey, MigrationBoundaryKey). The tree is
// materialized only on the new database (flatkv); memiavl never owns a
// "migration" tree and never stores migration metadata. External
// writes and reads against this name are rejected at every layer.
const MigrationStore = "migration"

const (
	// The key where the migration boundary is stored.
	//
	// This key always lives in the new database.
	MigrationBoundaryKey = "migration-boundary"

	// MigrationVersionKey stores the current migration version as an
	// 8-byte BigEndian uint64. An absent key is interpreted as
	// startVersion (the pre-migration state for the active mode). This
	// key is the source of truth for "which migration, if any, is
	// active" and survives across process restarts.
	//
	// Ownership: MigrationManager writes this key to the new database
	// exactly once per migration lifecycle, on the first ApplyChangeSets
	// call after the migration boundary reaches Complete ("bump block").
	// The same write atomically deletes MigrationBoundaryKey from the
	// new database. The key is never written to memiavl.
	MigrationVersionKey = "migration-version"
)
