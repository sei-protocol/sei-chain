package pebbledb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/bloom"
	"github.com/cockroachdb/pebble/v2/sstable"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/flatcache"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

const metricsScrapeInterval = 10 * time.Second

// pebbleDB implements the db_engine.DB interface using PebbleDB.
type pebbleDB struct {
	db            *pebble.DB
	metricsCancel context.CancelFunc
	cache         flatcache.Cache
}

var _ types.KeyValueDB = (*pebbleDB)(nil)

// TODO create a config struct for this!

// Open opens (or creates) a Pebble-backed DB at path, returning the DB interface.
func Open(
	ctx context.Context,
	path string,
	opts types.OpenOptions,
	enableMetrics bool,
	// A work pool for reading from the DB.
	readPool *utils.WorkPool,
	cacheSize int,
	pageCacheSize int,
) (_ types.KeyValueDB, err error) {
	// Validate options before allocating resources to avoid leaks on validation failure
	var cmp *pebble.Comparer
	if opts.Comparer != nil {
		var ok bool
		cmp, ok = opts.Comparer.(*pebble.Comparer)
		if !ok {
			return nil, fmt.Errorf("OpenOptions.Comparer must be *pebble.Comparer, got %T", opts.Comparer)
		}
	}

	// Internal pebbleDB cache, used to cache pages in memory. // TODO verify accuracy of this statement
	pebbleCache := pebble.NewCache(int64(pageCacheSize))
	defer pebbleCache.Unref()

	popts := &pebble.Options{
		Cache:    pebbleCache,
		Comparer: cmp,
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

	db, err := pebble.Open(path, popts)
	if err != nil {
		return nil, err
	}

	readFunction := func(key []byte) []byte { // TODO error handling!
		val, closer, err := db.Get(key)
		if err != nil {
			return nil
		}
		cloned := bytes.Clone(val)
		_ = closer.Close()
		return cloned
	}

	// A high level cache per key.
	cache, err := flatcache.NewCache(
		ctx,
		readFunction,
		8,
		cacheSize,
		readPool,
		10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to create flatcache: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	if enableMetrics {
		metrics.NewPebbleMetrics(ctx, db, filepath.Base(path), metricsScrapeInterval)
	}

	return &pebbleDB{
		db:            db,
		metricsCancel: cancel,
		cache:         cache,
	}, nil
}

func (p *pebbleDB) Get(key []byte) ([]byte, error) {
	// // Pebble returns a zero-copy view plus a closer; we copy and close internally.
	// val, closer, err := p.db.Get(key)
	// if err != nil {
	// 	if errors.Is(err, pebble.ErrNotFound) {
	// 		return nil, errorutils.ErrNotFound
	// 	}
	// 	return nil, err
	// }
	// cloned := bytes.Clone(val)
	// _ = closer.Close()

	val, found, err := p.cache.Get(key, true)
	if err != nil {
		return nil, fmt.Errorf("failed to get value from cache: %w", err)
	}
	if !found {
		return nil, errorutils.ErrNotFound
	}

	return val, nil
}

func (p *pebbleDB) BatchGet(keys map[string]types.BatchGetResult) {
	p.cache.BatchGet(keys)
}

func (p *pebbleDB) Set(key, value []byte, opts types.WriteOptions) error {
	// TODO batch set!
	p.cache.Set(key, value)
	return p.db.Set(key, value, toPebbleWriteOpts(opts))
}

func (p *pebbleDB) Delete(key []byte, opts types.WriteOptions) error {
	// TODO batch delete!
	p.cache.Delete(key)
	return p.db.Delete(key, toPebbleWriteOpts(opts))
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
	return p.db.Flush()
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
