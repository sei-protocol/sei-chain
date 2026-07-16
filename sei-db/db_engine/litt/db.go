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
	// BuildTable opens the table named by config.Name, creating one on disk if it does not exist. It must be
	// called exactly once per table per DB lifetime: calling BuildTable for a table that is already open
	// returns an error. The caller owns the returned handle.
	//
	// The supplied TableConfig is validated (see TableConfig.Validate); an invalid name or sharding factor
	// results in an error. Use DefaultTableConfig(name) to obtain a config with sane defaults. With the
	// exception of the name, none of the config settings are persisted to disk, so they are not retained across
	// restarts and must be supplied again (or changed via the Table setters) each time the database is
	// started/restarted.
	BuildTable(config TableConfig) (Table, error)

	// Close stops the database. This method must be called when the database is no longer needed.
	// Close ensures that all non-flushed data is crash durable on disk before returning. Calls to
	// Put() concurrent with Close() may not be crash durable after Close() returns.
	Close() error

	// Destroy deletes all data in the database.
	Destroy() error
}
