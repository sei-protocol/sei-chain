//go:build rocksdbBackend
// +build rocksdbBackend

package mvcc

import (
	"encoding/binary"
	"runtime"

	"github.com/linxGnu/grocksdb"
)

const (
	// CFNameStateStorage defines the RocksDB column family name for versioned state
	// storage.
	CFNameStateStorage = "state_storage"

	// CFNameDefault defines the RocksDB column family name for the default column.
	CFNameDefault = "default"
)

// NewRocksDBOpts returns the options used for the RocksDB column family for use
// in state storage.
//
// FIXME: We do not enable dict compression for SSTFileWriter, because otherwise
// the file writer won't report correct file size.
// Ref: https://github.com/facebook/rocksdb/issues/11146
func NewRocksDBOpts(sstFileWriter bool) *grocksdb.Options {
	opts := grocksdb.NewDefaultOptions()
	opts.SetCreateIfMissing(true)
	opts.SetComparator(CreateTSComparator())
	opts.IncreaseParallelism(runtime.NumCPU())
	opts.OptimizeLevelStyleCompaction(512 * 1024 * 1024)
	opts.SetTargetFileSizeMultiplier(2)
	opts.SetLevelCompactionDynamicLevelBytes(true)

	// block based table options
	bbto := grocksdb.NewDefaultBlockBasedTableOptions()

	// 1G block cache
	bbto.SetBlockSize(32 * 1024)
	bbto.SetBlockCache(grocksdb.NewLRUCache(1 << 30))

	bbto.SetFilterPolicy(grocksdb.NewRibbonHybridFilterPolicy(9.9, 1))
	bbto.SetIndexType(grocksdb.KBinarySearchWithFirstKey)
	bbto.SetOptimizeFiltersForMemory(true)
	opts.SetBlockBasedTableFactory(bbto)

	// Improve sst file creation speed: compaction or sst file writer.
	opts.SetCompressionOptionsParallelThreads(4)

	if !sstFileWriter {
		// compression options at bottommost level
		opts.SetBottommostCompression(grocksdb.ZSTDCompression)

		compressOpts := grocksdb.NewDefaultCompressionOptions()
		compressOpts.MaxDictBytes = 112640 // 110k
		compressOpts.Level = 12

		opts.SetBottommostCompressionOptions(compressOpts, true)
		opts.SetBottommostCompressionOptionsZstdMaxTrainBytes(compressOpts.MaxDictBytes*100, true)
	}

	return opts
}

// OpenRocksDB opens a RocksDB database connection for versioned reading and writing.
// It returns every opened column family handle so the caller can destroy them
// all at close: RocksDB requires the default column family to be opened too, so
// the first handle is the unused default while the second is "state_storage",
// the column family used for versioning with user-defined timestamps.
func OpenRocksDB(dataDir string) (*grocksdb.DB, []*grocksdb.ColumnFamilyHandle, error) {
	opts := grocksdb.NewDefaultOptions()
	opts.SetCreateIfMissing(true)
	opts.SetCreateIfMissingColumnFamilies(true)

	db, cfHandles, err := grocksdb.OpenDbColumnFamilies(
		opts,
		dataDir,
		[]string{
			CFNameDefault,
			CFNameStateStorage,
		},
		[]*grocksdb.Options{
			opts,
			NewRocksDBOpts(false),
		},
	)
	if err != nil {
		return nil, nil, err
	}

	return db, cfHandles, nil
}

// OpenRocksDBAndTrimHistory opens a RocksDB handle similar to `OpenRocksDB`,
// but it also trims the versions newer than target one, such that it can be used
// for rollback.
func OpenRocksDBAndTrimHistory(dataDir string, version int64) (*grocksdb.DB, []*grocksdb.ColumnFamilyHandle, error) {
	var ts [TimestampSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(version))

	opts := grocksdb.NewDefaultOptions()
	opts.SetCreateIfMissing(true)
	opts.SetCreateIfMissingColumnFamilies(true)

	db, cfHandles, err := grocksdb.OpenDbAndTrimHistory(
		opts,
		dataDir,
		[]string{
			CFNameDefault,
			CFNameStateStorage,
		},
		[]*grocksdb.Options{
			opts,
			NewRocksDBOpts(false),
		},
		ts[:],
	)
	if err != nil {
		return nil, nil, err
	}

	return db, cfHandles, nil
}
