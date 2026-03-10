package pebbledb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/bloom"
	"github.com/cockroachdb/pebble/v2/sstable"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/pebblecache"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// pebbleDB implements the db_engine.DB interface using PebbleDB.
type pebbleDB struct {
	db            *pebble.DB
	metricsCancel context.CancelFunc
	cache         pebblecache.Cache
}

var _ types.KeyValueDB = (*pebbleDB)(nil)

// Open opens (or creates) a Pebble-backed DB at path, returning the DB interface.
func Open(
	ctx context.Context,
	config *PebbleDBConfig,
	// Used to determine the ordering of keys in the database.
	comparer *pebble.Comparer,
	// A work pool for reading from the DB.
	readPool threading.Pool,
	// A work pool for miscellaneous operations that are neither computationally intensive nor IO bound.
	miscPool threading.Pool,
) (_ types.KeyValueDB, err error) {

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	// Internal pebbleDB block cache, used to cache uncompressed SSTable data blocks in memory.
	pebbleCache := pebble.NewCache(int64(config.BlockCacheSize))
	defer pebbleCache.Unref()

	popts := &pebble.Options{
		Cache:    pebbleCache,
		Comparer: comparer,
		// FormatMajorVersion is pinned to a specific version to prevent accidental
		// breaking changes when updating the pebble dependency. Using FormatNewest
		// would cause the on-disk format to silently upgrade when pebble is updated,
		// making the database incompatible with older software versions.
		// When upgrading this version, ensure it's an intentional, documented change.
		FormatMajorVersion:          pebble.FormatVirtualSSTables,
		L0CompactionThreshold:       4,
		L0StopWritesThreshold:       1000,
		LBaseMaxBytes:               64 << 20, // 64 MB
		MemTableSize:                64 << 20,
		MemTableStopWritesThreshold: 4,
		DisableWAL:                  false,
	}

	// Configure L0 with explicit settings
	popts.Levels[0].BlockSize = 32 << 10       // 32 KB
	popts.Levels[0].IndexBlockSize = 256 << 10 // 256 KB
	popts.Levels[0].FilterPolicy = bloom.FilterPolicy(10)
	popts.Levels[0].FilterType = pebble.TableFilter
	popts.Levels[0].Compression = func() *sstable.CompressionProfile { return sstable.ZstdCompression }
	popts.Levels[0].EnsureL0Defaults()

	// Configure L1+ levels, inheriting from previous level
	for i := 1; i < len(popts.Levels); i++ {
		l := &popts.Levels[i]
		l.BlockSize = 32 << 10       // 32 KB
		l.IndexBlockSize = 256 << 10 // 256 KB
		l.FilterPolicy = bloom.FilterPolicy(10)
		l.FilterType = pebble.TableFilter
		l.Compression = func() *sstable.CompressionProfile { return sstable.ZstdCompression }
		l.EnsureL1PlusDefaults(&popts.Levels[i-1])
	}

	// Disable bloom filter at bottommost level (L6) - bloom filters are less useful
	// at the bottom level since most data lives there and false positive rate is low
	popts.Levels[6].FilterPolicy = nil

	db, err := pebble.Open(config.DataDir, popts)
	if err != nil {
		return nil, err
	}

	readFunction := func(key []byte) ([]byte, bool, error) {
		val, closer, err := db.Get(key)
		if err != nil {
			if errors.Is(err, pebble.ErrNotFound) {
				return nil, false, nil
			}
			return nil, false, fmt.Errorf("failed to read from pebble: %w", err)
		}
		cloned := bytes.Clone(val)
		_ = closer.Close()
		return cloned, true, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	if config.EnableMetrics {
		NewPebbleMetrics(ctx, db, filepath.Base(config.DataDir), config.MetricsScrapeInterval)
	}

	var cache pebblecache.Cache
	if config.CacheSize == 0 {
		cache = pebblecache.NewNoOpCache(readFunction)
	} else {
		var cacheName string
		if config.EnableMetrics {
			cacheName = filepath.Base(config.DataDir)
		}

		cache, err = pebblecache.NewCache(
			ctx,
			readFunction,
			config.CacheShardCount,
			config.CacheSize,
			readPool,
			miscPool,
			cacheName,
			config.MetricsScrapeInterval)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create flatcache: %w", err)
		}
	}

	return &pebbleDB{
		db:            db,
		metricsCancel: cancel,
		cache:         cache,
	}, nil
}

func (p *pebbleDB) Get(key []byte) ([]byte, error) {
	val, found, err := p.cache.Get(key, true)
	if err != nil {
		return nil, fmt.Errorf("failed to get value from cache: %w", err)
	}
	if !found {
		return nil, errorutils.ErrNotFound
	}

	return val, nil
}

func (p *pebbleDB) BatchGet(keys map[string]types.BatchGetResult) error {
	err := p.cache.BatchGet(keys)
	if err != nil {
		return fmt.Errorf("failed to get values from cache: %w", err)
	}
	return nil
}

func (p *pebbleDB) Set(key, value []byte, opts types.WriteOptions) error {
	err := p.db.Set(key, value, toPebbleWriteOpts(opts))
	if err != nil {
		return fmt.Errorf("failed to set value in database: %w", err)
	}
	p.cache.Set(key, value)
	return nil
}

func (p *pebbleDB) Delete(key []byte, opts types.WriteOptions) error {
	err := p.db.Delete(key, toPebbleWriteOpts(opts))
	if err != nil {
		return fmt.Errorf("failed to delete value in database: %w", err)
	}
	p.cache.Delete(key)
	return nil
}

func (p *pebbleDB) NewIter(opts *types.IterOptions) (types.KeyValueDBIterator, error) {
	var iopts *pebble.IterOptions
	if opts != nil {
		iopts = &pebble.IterOptions{
			LowerBound: opts.LowerBound,
			UpperBound: opts.UpperBound,
		}
	}
	it, err := p.db.NewIter(iopts)
	if err != nil {
		return nil, err
	}
	return &pebbleIterator{it: it}, nil
}

func (p *pebbleDB) Flush() error {
	err := p.db.Flush()
	if err != nil {
		return fmt.Errorf("failed to flush database: %w", err)
	}

	return nil
}

func (p *pebbleDB) Checkpoint(destDir string) error {
	if p.db == nil {
		return errors.New("pebbleDB: checkpoint on closed database")
	}
	return p.db.Checkpoint(destDir, pebble.WithFlushedWAL())
}

var _ types.Checkpointable = (*pebbleDB)(nil)

func (p *pebbleDB) Close() error {
	// Make Close idempotent: Pebble panics if Close is called twice.
	if p.db == nil {
		return nil
	}

	if p.metricsCancel != nil {
		p.metricsCancel()
		p.metricsCancel = nil
	}

	db := p.db
	p.db = nil

	return db.Close()
}

func toPebbleWriteOpts(opts types.WriteOptions) *pebble.WriteOptions {
	if opts.Sync {
		return pebble.Sync
	}
	return pebble.NoSync
}
