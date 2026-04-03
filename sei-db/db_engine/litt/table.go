package litt

import (
	"regexp"
	"time"

	"github.com/Layr-Labs/eigenda/litt/types"
)

// TableNameRegex is a regular expression that matches valid table names.
var TableNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Table is a key-value store with a namespace that does not overlap with other tables.
// Values may be written to the table, but once written, they may not be changed or deleted (except via TTL).
//
// All methods in this interface are thread safe.
type Table interface {
	// Name returns the name of the table. Table names are unique across the database.
	Name() string

	// Put stores a value in the database. May not be used to overwrite an existing value.
	// Note that when this method returns, data written may not be crash durable on disk
	// (although the write does have atomicity). In order to ensure crash durability, call Flush().
	//
	// The maximum size of the key is 2^32 bytes. The maximum size of the value is 2^32 bytes.
	// This database has been optimized under the assumption that values are generally much larger than keys.
	// This affects performance, but not correctness.
	//
	// It is not safe to modify the byte slices passed to this function after the call
	// (both the key and the value).
	Put(key []byte, value []byte) error

	// PutBatch stores multiple values in the database. Similar to Put, but allows for multiple values to be written
	// at once. This may improve performance, but it otherwise has identical properties to a sequence of Put calls
	// (i.e. this method does not atomically write the entire batch).
	//
	// The maximum size of a key is 2^32 bytes. The maximum size of a value is 2^32 bytes.
	// This database has been optimized under the assumption that values are generally much larger than keys.
	// This affects performance, but not correctness.
	//
	// It is not safe to modify the byte slices passed to this function after the call
	// (including the key byte slices and the value byte slices).
	PutBatch(batch []*types.KVPair) error

	// Get retrieves a value from the database. The returned boolean indicates whether the key exists in the database
	// (returns false if the key does not exist). If an error is returned, the value of the other returned values are
	// undefined.
	//
	// The maximum size of a key is 2^32 bytes. The maximum size of a value is 2^32 bytes.
	// This database has been optimized under the assumption that values are generally much larger than keys.
	// This affects performance, but not correctness.
	//
	// For the sake of performance, the returned data is NOT safe to mutate. If you need to modify the data,
	// make a copy of it first. It is also not safe to modify the key byte slice after it is passed to this
	// method.
	Get(key []byte) (value []byte, exists bool, err error)

	// CacheAwareGet is identical to Get, except that it permits the caller to determine whether the value
	// should still be read if it is not present in the cache. If read, it also returns whether the value
	// was present in the cache. Note that the 'exists' return value is always accurate even if onlyReadFromCache
	// is true. If onlyReadFromCache is true and the value exists but is not in the cache, the returned values are
	// (nil, true, false, nil).
	CacheAwareGet(key []byte, onlyReadFromCache bool) (value []byte, exists bool, hot bool, err error)

	// Exists returns true if the key exists in the database, and false otherwise. This is faster than calling Get.
	//
	// It is not safe to modify the key byte slice after it is passed to this method.
	Exists(key []byte) (exists bool, err error)

	// Flush ensures that all data written to the database is crash durable on disk. When this method returns,
	// all data written by Put() operations is guaranteed to be crash durable. Put() operations that overlap with calls
	// to Flush() may not be crash durable after this method returns.
	//
	// Note that data flushed at the same time is not atomic. If the process crashes mid-flush, some data
	// being flushed may become persistent, while some may not. Each individual key-value pair is atomic
	// in the event of a crash, though. This is true even for very large keys/values.
	Flush() error

	// Size returns the disk size of the table in bytes. Does not include the size of any data stored only in memory.
	//
	// Note that the value returned by this method may lag slightly behind the actual size of the table due to the
	// pipelined implementation of the database. If an exact size is needed, first call Flush(), then call Size().
	//
	// Due to technical limitations, this size may or may not accurately reflect the size of the keymap. This is
	// because some third party libraries used for certain keymap implementations do not provide an accurate way to
	// measure size.
	Size() uint64

	// KeyCount returns the number of keys in the table.
	KeyCount() uint64

	// SetTTL sets the time to live for data in this table. This TTL is immediately applied to data already in
	// the table. Note that deletion is lazy. That is, when the data expires, it may not be deleted immediately.
	//
	// A TTL less than or equal to 0 means that the data never expires.
	SetTTL(ttl time.Duration) error

	// SetShardingFactor sets the number of write shards used. Increasing this value increases the number of parallel
	// writes that can be performed.
	SetShardingFactor(shardingFactor uint32) error

	// SetWriteCacheSize sets the write cache size, in bytes, for the table. For table implementations without a cache,
	// this method does nothing. The cache is used to store recently written data. When reading from the table,
	// if the requested data is present in this cache, the cache is used instead of reading from disk. Reading from the
	// cache is significantly faster than reading from the disk.
	//
	// If the cache size is set to 0 (default), the cache is disabled. The size of each cache entry is equal to the sum
	// of key length and the value length. Note that the actual in-memory footprint of the cache will be slightly
	// larger than the cache size due to implementation overhead (e.g. pointers, slice headers, map entries, etc.).
	SetWriteCacheSize(size uint64) error

	// SetReadCacheSize sets the read cache size, in bytes, for the table. For table implementations without a cache,
	// this method does nothing. The cache is used to store recently read data. When reading from the table,
	// if the requested data is present in this cache, the cache is used instead of reading from disk. Reading from the
	// cache is significantly faster than reading from the disk.
	//
	// If the cache size is set to 0 (default), the cache is disabled. The size of each cache entry is equal to the sum
	// of key length and the value length. Note that the actual in-memory footprint of the cache will be slightly
	// larger than the cache size due to implementation overhead (e.g. pointers, slice headers, map entries, etc.).
	SetReadCacheSize(size uint64) error
}

// isTableNameValid returns true if the table name is valid.
func IsTableNameValid(name string) bool {
	return TableNameRegex.MatchString(name)
}

// ManagedTable is a Table that can perform garbage collection on its data. This type should not be directly used
// by clients, and is a type that is used internally by the database.
type ManagedTable interface {
	Table

	// Close shuts down the table, flushing data to disk.
	Close() error

	// Destroy cleans up resources used by the table. All data on disk is permanently and unrecoverable deleted.
	Destroy() error

	// RunGC performs a garbage collection run. This method blocks until that run is complete.
	// This method is intended for use in tests, where it can be useful to force a garbage collection run to occur
	// at a specific time.
	RunGC() error
}
