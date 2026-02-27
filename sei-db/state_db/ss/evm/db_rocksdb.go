//go:build rocksdbBackend
// +build rocksdbBackend

package evm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"runtime"
	"sync/atomic"

	"github.com/linxGnu/grocksdb"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

func init() {
	RegisterEVMBackend("rocksdb", func(dir string, storeType EVMStoreType) (EVMDBEngine, error) {
		return OpenRocksDB(dir, storeType)
	})
}

const (
	rocksTimestampSize = 8

	rocksCFDefault      = "default"
	rocksCFStateStorage = "state_storage"

	rocksLatestVersionKey   = "s/latest"
	rocksEarliestVersionKey = "s/earliest"
)

var (
	rocksDefaultWriteOpts = grocksdb.NewDefaultWriteOptions()
	rocksDefaultReadOpts  = grocksdb.NewDefaultReadOptions()
)

// EVMDatabaseRocksDB is a RocksDB-backed versioned KV store for a single EVM data type.
// Uses user-defined timestamps for MVCC versioning and a column family for state data.
type EVMDatabaseRocksDB struct {
	storage  *grocksdb.DB
	cfHandle *grocksdb.ColumnFamilyHandle

	tsLow           int64
	latestVersion   atomic.Int64
	earliestVersion atomic.Int64
}

// OpenRocksDB opens a RocksDB instance for one EVM sub-database.
func OpenRocksDB(dir string, storeType EVMStoreType) (*EVMDatabaseRocksDB, error) {
	dbPath := filepath.Join(dir, StoreTypeName(storeType))

	defaultOpts := grocksdb.NewDefaultOptions()
	defaultOpts.SetCreateIfMissing(true)
	defaultOpts.SetCreateIfMissingColumnFamilies(true)
	defer defaultOpts.Destroy()

	cfOpts := newRocksDBEVMOpts()
	defer cfOpts.Destroy()

	db, cfHandles, err := grocksdb.OpenDbColumnFamilies(
		defaultOpts,
		dbPath,
		[]string{rocksCFDefault, rocksCFStateStorage},
		[]*grocksdb.Options{defaultOpts, cfOpts},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open EVM RocksDB %s: %w", StoreTypeName(storeType), err)
	}
	cfHandle := cfHandles[1]

	slice, err := db.GetFullHistoryTsLow(cfHandle)
	if err != nil {
		return nil, fmt.Errorf("failed to get full_history_ts_low: %w", err)
	}
	var tsLow int64
	tsLowBz := rocksCloneSlice(slice)
	if len(tsLowBz) > 0 {
		tsLow = int64(binary.LittleEndian.Uint64(tsLowBz))
	}

	earliest, err := rocksRetrieveVersion(db, rocksEarliestVersionKey)
	if err != nil {
		return nil, err
	}
	latest, err := rocksRetrieveVersion(db, rocksLatestVersionKey)
	if err != nil {
		return nil, err
	}

	evmDB := &EVMDatabaseRocksDB{
		storage:  db,
		cfHandle: cfHandle,
		tsLow:    tsLow,
	}
	evmDB.latestVersion.Store(latest)
	evmDB.earliestVersion.Store(earliest)

	return evmDB, nil
}

func newRocksDBEVMOpts() *grocksdb.Options {
	opts := grocksdb.NewDefaultOptions()
	opts.SetCreateIfMissing(true)
	opts.SetComparator(createTimestampComparator())
	opts.IncreaseParallelism(runtime.NumCPU())
	opts.OptimizeLevelStyleCompaction(512 * 1024 * 1024)
	opts.SetTargetFileSizeMultiplier(2)
	opts.SetLevelCompactionDynamicLevelBytes(true)

	bbto := grocksdb.NewDefaultBlockBasedTableOptions()
	bbto.SetBlockSize(32 * 1024)
	bbto.SetBlockCache(grocksdb.NewLRUCache(256 << 20)) // 256 MB per sub-DB
	bbto.SetFilterPolicy(grocksdb.NewRibbonHybridFilterPolicy(9.9, 1))
	bbto.SetIndexType(grocksdb.KBinarySearchWithFirstKey)
	bbto.SetOptimizeFiltersForMemory(true)
	opts.SetBlockBasedTableFactory(bbto)

	opts.SetCompressionOptionsParallelThreads(4)
	opts.SetBottommostCompression(grocksdb.ZSTDCompression)
	compressOpts := grocksdb.NewDefaultCompressionOptions()
	compressOpts.MaxDictBytes = 112640
	compressOpts.Level = 12
	opts.SetBottommostCompressionOptions(compressOpts, true)
	opts.SetBottommostCompressionOptionsZstdMaxTrainBytes(compressOpts.MaxDictBytes*100, true)

	return opts
}

func (db *EVMDatabaseRocksDB) Get(key []byte, version int64) ([]byte, error) {
	if version < db.earliestVersion.Load() {
		return nil, nil
	}
	readOpts := rocksNewTSReadOpts(version)
	defer readOpts.Destroy()

	slice, err := db.storage.GetCF(readOpts, db.cfHandle, key)
	if err != nil {
		return nil, fmt.Errorf("rocksdb evm get: %w", err)
	}
	return rocksCloneSlice(slice), nil
}

func (db *EVMDatabaseRocksDB) Has(key []byte, version int64) (bool, error) {
	if version < db.earliestVersion.Load() {
		return false, nil
	}
	readOpts := rocksNewTSReadOpts(version)
	defer readOpts.Destroy()

	slice, err := db.storage.GetCF(readOpts, db.cfHandle, key)
	if err != nil {
		return false, err
	}
	exists := slice.Exists()
	slice.Free()
	return exists, nil
}

func (db *EVMDatabaseRocksDB) Set(key, value []byte, version int64) error {
	var ts [rocksTimestampSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(version))

	batch := grocksdb.NewWriteBatch()
	batch.PutCFWithTS(db.cfHandle, key, ts[:], value)
	batch.Put([]byte(rocksLatestVersionKey), ts[:])
	defer batch.Destroy()

	if err := db.storage.Write(rocksDefaultWriteOpts, batch); err != nil {
		return err
	}
	db.latestVersion.Store(version)
	return nil
}

func (db *EVMDatabaseRocksDB) Delete(key []byte, version int64) error {
	var ts [rocksTimestampSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(version))

	batch := grocksdb.NewWriteBatch()
	batch.DeleteCFWithTS(db.cfHandle, key, ts[:])
	batch.Put([]byte(rocksLatestVersionKey), ts[:])
	defer batch.Destroy()

	if err := db.storage.Write(rocksDefaultWriteOpts, batch); err != nil {
		return err
	}
	db.latestVersion.Store(version)
	return nil
}

func (db *EVMDatabaseRocksDB) ApplyBatch(pairs []*iavl.KVPair, version int64) error {
	if len(pairs) == 0 {
		return nil
	}

	var ts [rocksTimestampSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(version))

	batch := grocksdb.NewWriteBatch()
	batch.Put([]byte(rocksLatestVersionKey), ts[:])
	for _, pair := range pairs {
		if pair.Value == nil || pair.Delete {
			batch.DeleteCFWithTS(db.cfHandle, pair.Key, ts[:])
		} else {
			batch.PutCFWithTS(db.cfHandle, pair.Key, ts[:], pair.Value)
		}
	}
	defer batch.Destroy()

	if err := db.storage.Write(rocksDefaultWriteOpts, batch); err != nil {
		return err
	}
	db.latestVersion.Store(version)
	return nil
}

func (db *EVMDatabaseRocksDB) GetLatestVersion() int64 {
	return db.latestVersion.Load()
}

func (db *EVMDatabaseRocksDB) SetLatestVersion(version int64) error {
	var ts [rocksTimestampSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(version))
	if err := db.storage.Put(rocksDefaultWriteOpts, []byte(rocksLatestVersionKey), ts[:]); err != nil {
		return err
	}
	db.latestVersion.Store(version)
	return nil
}

func (db *EVMDatabaseRocksDB) GetEarliestVersion() int64 {
	return db.earliestVersion.Load()
}

func (db *EVMDatabaseRocksDB) SetEarliestVersion(version int64) error {
	if version <= db.earliestVersion.Load() {
		return nil
	}
	var ts [rocksTimestampSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(version))
	if err := db.storage.Put(rocksDefaultWriteOpts, []byte(rocksEarliestVersionKey), ts[:]); err != nil {
		return err
	}
	db.earliestVersion.Store(version)
	return nil
}

// Prune leverages RocksDB's IncreaseFullHistoryTsLow for lazy pruning.
// Old versions are dropped during subsequent compactions.
func (db *EVMDatabaseRocksDB) Prune(version int64) error {
	if db.storage == nil {
		return fmt.Errorf("rocksdb: database is closed")
	}
	tsLow := version + 1
	var ts [rocksTimestampSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(tsLow))

	if err := db.storage.IncreaseFullHistoryTsLow(db.cfHandle, ts[:]); err != nil {
		return fmt.Errorf("failed to update full_history_ts_low: %w", err)
	}
	db.tsLow = tsLow
	return db.SetEarliestVersion(tsLow)
}

func (db *EVMDatabaseRocksDB) Close() error {
	if db.storage == nil {
		return nil
	}
	if db.cfHandle != nil {
		db.cfHandle.Destroy()
		db.cfHandle = nil
	}
	db.storage.Close()
	db.storage = nil
	return nil
}

// --- helpers ---

func rocksNewTSReadOpts(version int64) *grocksdb.ReadOptions {
	ts := make([]byte, rocksTimestampSize)
	binary.LittleEndian.PutUint64(ts, uint64(version))
	ro := grocksdb.NewDefaultReadOptions()
	ro.SetTimestamp(ts)
	return ro
}

func rocksRetrieveVersion(db *grocksdb.DB, key string) (int64, error) {
	bz, err := db.GetBytes(rocksDefaultReadOpts, []byte(key))
	if err != nil || len(bz) == 0 {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(bz)), nil
}

func rocksCloneSlice(s *grocksdb.Slice) []byte {
	defer s.Free()
	if !s.Exists() {
		return nil
	}
	out := make([]byte, len(s.Data()))
	copy(out, s.Data())
	return out
}

// --- timestamp comparator ---

// createTimestampComparator returns a comparator identical to the RocksDB builtin
// "leveldb.BytewiseComparator.u64ts" so that ldb/sst_dump can work with these DBs.
func createTimestampComparator() *grocksdb.Comparator {
	return grocksdb.NewComparatorWithTimestamp(
		"leveldb.BytewiseComparator.u64ts",
		rocksTimestampSize,
		rocksCompare,
		rocksCompareTS,
		rocksCompareWithoutTS,
	)
}

func rocksCompareTS(a, b []byte) int {
	ts1 := binary.LittleEndian.Uint64(a)
	ts2 := binary.LittleEndian.Uint64(b)
	switch {
	case ts1 < ts2:
		return -1
	case ts1 > ts2:
		return 1
	default:
		return 0
	}
}

func rocksCompare(a, b []byte) int {
	ret := rocksCompareWithoutTS(a, true, b, true)
	if ret != 0 {
		return ret
	}
	return -rocksCompareTS(a[len(a)-rocksTimestampSize:], b[len(b)-rocksTimestampSize:])
}

func rocksCompareWithoutTS(a []byte, aHasTS bool, b []byte, bHasTS bool) int {
	if aHasTS {
		a = a[:len(a)-rocksTimestampSize]
	}
	if bHasTS {
		b = b[:len(b)-rocksTimestampSize]
	}
	return bytes.Compare(a, b)
}
