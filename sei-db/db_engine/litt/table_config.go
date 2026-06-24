package litt

import (
	"fmt"
	"time"
)

// TableConfig is the per-table configuration supplied at table creation time via DB.BuildTable. With the
// exception of Name, none of these settings are persisted to disk: they apply only for the lifetime of the
// table handle and must be provided again (or changed via the Table setters) after each restart.
type TableConfig struct {
	// The name of the table. Required. Table names appear as directories on the file system, and so are
	// restricted to ASCII alphanumeric characters, dashes, and underscores. The name must be at least one
	// character long.
	Name string

	// The time-to-live for data in the table. The default is 0 (no TTL, data never expires). May be changed at
	// runtime via Table.SetTTL().
	TTL time.Duration

	// The sharding factor for the table. If greater than 1, then values will be spread out across multiple files.
	// (Note that individual values will always be written to a single file, but two different values may be written
	// to different files.) These shard files are spread evenly across the database paths. If the sharding factor is
	// larger than the number of paths, then some paths will have multiple shard files. If smaller, then some paths
	// may not always have an actively written shard file.
	//
	// The default is 8. Must be in the range [1, MaxShardingFactor]. Storing this as a uint8 makes it structurally
	// impossible to configure more shards than the on-disk format can address. May be changed at runtime via
	// Table.SetShardingFactor().
	//
	// Normally, writes to a table are individually atomic but not atomic in aggregate. That is to say, if a caller
	// writes A and then B and the DB crashes before flushing, it may be the case that B is persisted but A is not.
	// However, if the sharding factor is 1, then all writes are made crash durable in the order they were issued.
	ShardingFactor uint8

	// The size of the write cache, in bytes, for the table. A write cache stores recently written values for fast
	// access. The default is 0 (no cache). Cache size includes the size of both the key and the value. May be
	// changed at runtime via Table.SetWriteCacheSize().
	WriteCacheSize uint64

	// The size of the read cache, in bytes, for the table. A read cache stores recently read values for fast
	// access. The default is 0 (no cache). Cache size includes the size of both the key and the value. May be
	// changed at runtime via Table.SetReadCacheSize().
	ReadCacheSize uint64

	// A function that is called to determine if a key is eligible for garbage collection. Keys are GC eligible once
	// their TTL has expired, and once this function returns true. If nil, only TTL determines GC eligibility.
	GCFilter GCFilter
}

// GCFilter is a function that is called to determine if a key is eligible for garbage collection. Keys are GC
// eligible once their TTL has expired, and once this function returns true. Since GC is disabled if TTL is 0,
// this function is only called if TTL is greater than 0.
//
// This function must be monotonic. That is, once it returns true for a key, it must ALWAYS return true for that key.
//
// Returning an error from this function should be reserved for non-recoverable errors, such as data corruption
// or a failure to parse the key. Returning an error will cause the DB to crash (loudly, by design).
//
// If nil, only TTL determines GC eligibility.
type GCFilter func(key []byte, isPrimaryKey bool) (bool, error)

// DefaultTableConfig returns a TableConfig for a table with the given name and sane default values for all
// other settings.
func DefaultTableConfig(name string) TableConfig {
	return TableConfig{
		Name:           name,
		TTL:            0,
		ShardingFactor: 8,
		WriteCacheSize: 0,
		ReadCacheSize:  0,
	}
}

// Validate performs a sanity check on the table configuration, returning an error if any of the settings are
// invalid. The config returned by DefaultTableConfig() is guaranteed to pass this check if unmodified.
func (c *TableConfig) Validate() error {
	if !IsTableNameValid(c.Name) {
		return fmt.Errorf(
			"table name '%s' is invalid, must be at least one character long and "+
				"contain only letters, numbers, underscores, and dashes", c.Name)
	}
	if c.ShardingFactor < 1 {
		return fmt.Errorf("sharding factor must be at least 1")
	}
	return nil
}
