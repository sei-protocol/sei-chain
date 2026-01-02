package pebbledb

import (
	"github.com/pkg/errors"

	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/bloom"
)

// Database represents database.
type Database struct {
	storage  *pebble.DB
	writeOps *pebble.WriteOptions
}

// OpenDB opens an existing or create a new database.
func OpenDB(dbPath string) *Database {
	cache := pebble.NewCache(1024 * 1024 * 512)
	defer cache.Unref()
	opts := &pebble.Options{
		Cache:                       cache,
		FormatMajorVersion:          pebble.FormatNewest,
		L0CompactionThreshold:       4,
		L0StopWritesThreshold:       1000,
		LBaseMaxBytes:               64 << 20, // 64 MB
		Levels:                      make([]pebble.LevelOptions, 7),
		MaxConcurrentCompactions:    func() int { return 3 },
		MemTableSize:                64 << 20,
		MemTableStopWritesThreshold: 4,
		DisableWAL:                  false,
	}

	for i := range opts.Levels {
		l := &opts.Levels[i]
		l.BlockSize = 32 << 10       // 32 KB
		l.IndexBlockSize = 256 << 10 // 256 KB
		l.FilterPolicy = bloom.FilterPolicy(10)
		l.FilterType = pebble.TableFilter
		if i > 1 {
			l.Compression = pebble.ZstdCompression
		}
		if i > 0 {
			l.TargetFileSize = opts.Levels[i-1].TargetFileSize * 2
		}
		l.EnsureDefaults()
	}
	opts.Levels[6].FilterPolicy = nil
	opts.FlushSplitBytes = opts.Levels[0].TargetFileSize
	opts = opts.EnsureDefaults()

	db, err := pebble.Open(dbPath, opts)
	if err != nil {
		panic(err)
	}

	database := &Database{
		storage:  db,
		writeOps: pebble.NoSync,
	}

	return database
}

// Has checks if key is available.
func (db *Database) Has(key []byte) (bool, error) {
	val, err := db.Get(key)
	if err != nil {
		return false, errors.WithStack(err)
	}
	return val != nil, nil
}

// Get returns value by key.
func (db *Database) Get(key []byte) ([]byte, error) {
	value, closer, err := db.storage.Get(key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, nil
		}
		return nil, errors.WithStack(err)
	}
	return value, errors.WithStack(closer.Close())
}

// Set override and persist key,value pair.
func (db *Database) Set(key []byte, value []byte) error {
	return db.storage.Set(key, value, db.writeOps)
}

// Close closes the database.
func (db *Database) Close() error {
	_ = db.storage.Flush()
	err := db.storage.Close()
	return errors.WithStack(err)
}
