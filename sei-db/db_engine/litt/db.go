package litt

// DB is a highly specialized key-value store. It is intentionally very feature poor, sacrificing
// unnecessary features for simplicity, high performance, and low memory usage.
//
// Litt: adjective, slang, a synonym for "cool" or "awesome". e.g. "Man, that database is litt, bro!".
//
// Supported features:
//   - writing values
//   - reading values
//   - TTLs and automatic (lazy) deletion of expired values
//   - tables with non-overlapping namespaces
//   - thread safety: all methods are safe to call concurrently, and all key-value pair modifications are
//     individually atomic
//   - dynamic multi-drive support (data can be spread across multiple physical volumes, and
//     volume membership can be changed at runtime without stopping the DB)
//   - incremental backups (both local and remote)
//
// Unsupported features:
// - mutating existing values (once a value is written, it cannot be changed)
// - multi-entity atomicity (there is no supported way to atomically write multiple key-value pairs as a group)
// - deleting values (values only leave the DB when they expire via a TTL)
// - transactions (individual operations are atomic, but there is no way to group operations atomically)
// - fine granularity for TTL (all data in the same table must have the same TTL)
type DB interface {
	// GetTable gets a table by name, creating one if it does not exist.
	//
	// Table names appear as directories on the file system, and so table names are restricted to be
	// ASCII alphanumeric characters, dashes, and underscores. The name must be at least one character long.
	//
	// The first time a table is fetched (either a new table or an existing one loaded from disk), its TTL is always
	// set to 0 (i.e. it has no TTL, meaning data is never deleted). If you want to set a TTL, you must call
	// Table.SetTTL() to do so. This is necessary after each time the database is started/restarted.
	GetTable(name string) (Table, error)

	// DropTable deletes a table and all of its data. This is a no-op if the table does not exist.
	//
	// Note that it is NOT thread safe to drop a table concurrently with any operation that accesses the table.
	// The table returned by GetTable() before DropTable() is called must not be used once DropTable() is called.
	DropTable(name string) error

	// Size returns the on-disk size of the database in bytes.
	//
	// Note that this size may not accurately reflect the size of the keymap. This is because some third party
	// libraries used for certain keymap implementations do not provide an accurate way to measure size.
	Size() uint64

	// KeyCount returns the number of keys in the database.
	KeyCount() uint64

	// Close stops the database. This method must be called when the database is no longer needed.
	// Close ensures that all non-flushed data is crash durable on disk before returning. Calls to
	// Put() concurrent with Close() may not be crash durable after Close() returns.
	Close() error

	// Destroy deletes all data in the database.
	Destroy() error
}
