package litt

import (
	"regexp"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
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
	// Optional secondary keys may be supplied; each secondary key acts as an additional alias for a
	// sub-range of the value (or the whole value, when Offset=0 and Length=len(value)). Secondary
	// keys are first-class keys: they appear in KeyCount(), Get(), Exists(), and are subject to the
	// same TTL as the primary. They share the value's bytes on disk, so they cost one keymap entry
	// each and do not duplicate value bytes. Secondary keys must be globally unique just like
	// primary keys, and must not collide with the primary key or other secondaries.
	//
	// The maximum size of a key (primary or secondary) is 64 KiB (2^16 - 1 bytes). The maximum size
	// of the value is 2^32 bytes. This database has been optimized under the assumption that values
	// are generally much larger than keys. This affects performance, but not correctness.
	//
	// Although writes are individually atomic, the DB makes no guarantees about atomicity of multiple writes in
	// aggregate. That is to say, if a caller writes A and then B and the DB crashes before flushing, it may be the
	// case that B is persisted but A is not. The exception to this rule is if the sharding factor for this table
	// is 1, in which case the database guarantees that writes become crash durable in the order they were issued.
	//
	// It is not safe to modify the byte slices passed to this function after the call
	// (the key bytes, the value bytes, and every secondary key's bytes).
	Put(key []byte, value []byte, secondaryKeys ...*types.SecondaryKey) error

	// PutBatch stores multiple values in the database. Similar to Put, but allows for multiple values to be written
	// at once, which may improve performance.
	//
	// Each PutRequest may include zero or more secondary keys (see Put for semantics).
	//
	// The maximum size of a key (primary or secondary) is 64 KiB (2^16 - 1 bytes). The maximum size
	// of a value is 2^32 bytes. This database has been optimized under the assumption that values
	// are generally much larger than keys. This affects performance, but not correctness.
	//
	// Although writes in a batch are individually atomic, the DB makes no guarantees about atomicity of multiple
	// writes in aggregate. That is to say, if a caller writes A and then B in a batch and the DB crashes before
	// flushing, it may be the case that B is persisted but A is not. The exception to this rule is if the sharding
	// factor for this table is 1, in which case the database guarantees that writes become crash durable in the
	// order they were issued.
	//
	// It is not safe to modify the byte slices passed to this function after the call
	// (including the key byte slices, the value byte slices, and every secondary key's bytes).
	PutBatch(batch []*types.PutRequest) error

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
	// writes that can be performed. Must be in the range [1, MaxShardingFactor].
	SetShardingFactor(shardingFactor uint8) error

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

	// Drop deletes the table and all of its data. All data on disk is permanently and unrecoverably deleted.
	// Once Drop is called, this handle must no longer be used.
	//
	// Note that it is NOT thread safe to drop a table concurrently with any other operation that accesses the
	// table. The owning DB will lazily forget a dropped table, after which its name may be reused via
	// BuildTable.
	Drop() error

	// Iterator returns a new iterator over the keys in the table. If reverse is false, iteration proceeds
	// in insertion order (oldest key first); if reverse is true, iteration proceeds in reverse insertion
	// order (newest key first).
	//
	// Forward iteration (reverse == false) is highly efficient for linearly scanning through keys and
	// values, reading from disk roughly sequentially. Reverse iteration may incur nontrivial random IO
	// when reading values, because the on-disk data layout does not support efficient backward scans.
	//
	// Creating an iterator has a moderately small performance impact, but creating iterators frequently
	// may have a significant performance impact. Iterators are not designed to be instantiated once a
	// second or more frequently. Additinoally, iteration disables GC temporarily. If frequent iteration
	// after startup time ever becomes an important access pattern, some tweaks to iteration and GC
	// implementation will be needed.
	//
	// The returned iterator captures a snapshot of the keys present when it is created; keys written after
	// the iterator is created are not observed. The iterator MUST be closed when no longer needed (see
	// Iterator.Close).
	Iterator(reverse bool) (Iterator, error)

	// GetOldestKey returns the oldest (earliest inserted) primary key in the table that has not been
	// deleted. The returned boolean is false if the table contains no keys.
	//
	// It is not safe to modify the returned key byte slice.
	GetOldestKey() (key []byte, exists bool, err error)

	// GetNewestKey returns the newest (most recently inserted) primary key in the table. The returned
	// boolean is false if the table contains no keys.
	//
	// It is not safe to modify the returned key byte slice.
	GetNewestKey() (key []byte, exists bool, err error)
}

// Iterator iterates over the keys in a table in insertion order (or reverse insertion order). It is
// created via Table.Iterator.
//
// An Iterator is NOT safe for concurrent use by multiple goroutines.
type Iterator interface {
	// Next advances the iterator to the next key. It returns false when the iteration is complete (no
	// more keys), and returns an error if advancing failed. After Next returns (false, nil), iteration
	// is complete; after it returns an error, the iterator must not be used further (other than Close).
	Next() (bool, error)

	// GetKey returns the current key and whether it is a primary key (as opposed to a secondary key).
	// It is only valid to call GetKey after Next has returned (true, nil). The returned key must not be
	// modified.
	GetKey() (key []byte, isPrimary bool)

	// GetValue reads and returns the value associated with the current key. It is only valid to call
	// GetValue after Next has returned (true, nil). The returned value must not be modified.
	GetValue() (value []byte, err error)

	// Close releases the resources held by the iterator.
	//
	// MUST be called when done. Failure to close an iterator may result in an unbounded disk leak.
	Close() error
}

// isTableNameValid returns true if the table name is valid.
func IsTableNameValid(name string) bool {
	return TableNameRegex.MatchString(name)
}

// This type should not be directly used by clients, and is a type that is used internally by the database.
type ManagedTable interface {
	Table

	// Close shuts down the table, flushing data to disk.
	Close() error

	// IsDropped returns true if the table has been dropped (see Table.Drop). A dropped table's data has been
	// deleted and the table must no longer be used. This is used internally by the DB to forget dropped tables.
	IsDropped() bool

	// RunGC performs a garbage collection run. This method blocks until that run is complete.
	// This method is intended for use in tests, where it can be useful to force a garbage collection run to occur
	// at a specific time.
	RunGC() error
}
