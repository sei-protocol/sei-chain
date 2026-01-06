package pebbledb

import (
	"fmt"
	"io"

	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/bloom"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine"
)

// pebbleDB implements the db_engine.DB interface using PebbleDB.
type pebbleDB struct {
	db    *pebble.DB
	cache *pebble.Cache
}

var _ db_engine.DB = (*pebbleDB)(nil)

// Open opens (or creates) a Pebble-backed DB at path, returning the DB interface.
func Open(path string, opts db_engine.OpenOptions) (db_engine.DB, error) {
	// Cache is reference-counted. We keep it alive for the lifetime of the DB and
	// Unref it in (*pebbleDB).Close(). Only on Open() failure do we Unref early.
	cache := pebble.NewCache(1024 * 1024 * 512) // 512MB cache

	popts := &pebble.Options{
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

	if opts.Comparer != nil {
		cmp, ok := opts.Comparer.(*pebble.Comparer)
		if !ok {
			return nil, fmt.Errorf("OpenOptions.Comparer must be *pebble.Comparer, got %T", opts.Comparer)
		}
		popts.Comparer = cmp
	}

	for i := 0; i < len(popts.Levels); i++ {
		l := &popts.Levels[i]
		l.BlockSize = 32 << 10       // 32 KB
		l.IndexBlockSize = 256 << 10 // 256 KB
		l.FilterPolicy = bloom.FilterPolicy(10)
		l.FilterType = pebble.TableFilter
		if i > 1 {
			l.Compression = pebble.ZstdCompression
		}
		if i > 0 {
			l.TargetFileSize = popts.Levels[i-1].TargetFileSize * 2
		}
		l.EnsureDefaults()
	}

	popts.Levels[6].FilterPolicy = nil
	popts.FlushSplitBytes = popts.Levels[0].TargetFileSize
	popts = popts.EnsureDefaults()

	db, err := pebble.Open(path, popts)
	if err != nil {
		cache.Unref() // only unref manually if we fail to open the db
		return nil, err
	}
	return &pebbleDB{db: db, cache: cache}, nil
}

func (p *pebbleDB) Get(key []byte) ([]byte, io.Closer, error) {
	// Pebble returns a zero-copy view plus a closer; see db_engine.DB contract.
	val, closer, err := p.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, nil, db_engine.ErrNotFound
		}
		return nil, nil, err
	}
	return val, closer, nil
}

func (p *pebbleDB) Set(key, value []byte, opts db_engine.WriteOptions) error {
	return p.db.Set(key, value, toPebbleWriteOpts(opts))
}

func (p *pebbleDB) Delete(key []byte, opts db_engine.WriteOptions) error {
	return p.db.Delete(key, toPebbleWriteOpts(opts))
}

func (p *pebbleDB) NewIter(opts *db_engine.IterOptions) (db_engine.Iterator, error) {
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

func (p *pebbleDB) Close() error {
	err := p.db.Close()
	if p.cache != nil {
		p.cache.Unref()
	}
	return err
}

func toPebbleWriteOpts(opts db_engine.WriteOptions) *pebble.WriteOptions {
	if opts.Sync {
		return pebble.Sync
	}
	return pebble.NoSync
}
