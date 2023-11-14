package pebbledb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/bloom"
	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/sei-protocol/sei-db/common/utils"
	"github.com/sei-protocol/sei-db/proto"
	sstypes "github.com/sei-protocol/sei-db/ss/types"
	"golang.org/x/exp/slices"
)

const (
	VersionSize = 8

	StorePrefixTpl   = "s/k:%s/"   // s/k:<storeKey>
	latestVersionKey = "s/_latest" // NB: latestVersionKey key must be lexically smaller than StorePrefixTpl
	tombstoneVal     = "TOMBSTONE"

	// TODO: Make configurable
	ImportCommitBatchSize = 10000
)

var (
	_ sstypes.StateStore = (*Database)(nil)

	defaultWriteOpts = pebble.NoSync
)

type Database struct {
	storage *pebble.DB
}

func New(dataDir string) (*Database, error) {
	cache := pebble.NewCache(1024 * 1024 * 32)
	defer cache.Unref()
	opts := &pebble.Options{
		Cache:                       cache,
		Comparer:                    MVCCComparer,
		FormatMajorVersion:          pebble.FormatNewest,
		L0CompactionThreshold:       2,
		L0StopWritesThreshold:       1000,
		LBaseMaxBytes:               64 << 20, // 64 MB
		Levels:                      make([]pebble.LevelOptions, 7),
		MaxConcurrentCompactions:    func() int { return 3 }, // TODO: Make Configurable
		MemTableSize:                64 << 20,
		MemTableStopWritesThreshold: 4,
	}

	for i := 0; i < len(opts.Levels); i++ {
		l := &opts.Levels[i]
		l.BlockSize = 32 << 10       // 32 KB
		l.IndexBlockSize = 256 << 10 // 256 KB
		l.FilterPolicy = bloom.FilterPolicy(10)
		l.FilterType = pebble.TableFilter
		// TODO: Consider compression only for specific layers like bottommost
		l.Compression = pebble.ZstdCompression
		if i > 0 {
			l.TargetFileSize = opts.Levels[i-1].TargetFileSize * 2
		}
		l.EnsureDefaults()
	}

	opts.Levels[6].FilterPolicy = nil
	opts.FlushSplitBytes = opts.Levels[0].TargetFileSize
	opts = opts.EnsureDefaults()

	db, err := pebble.Open(dataDir, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open PebbleDB: %w", err)
	}

	return &Database{
		storage: db,
	}, nil
}

func NewWithDB(storage *pebble.DB) *Database {
	return &Database{
		storage: storage,
	}
}

func (db *Database) Close() error {
	err := db.storage.Close()
	db.storage = nil
	return err
}

func (db *Database) SetLatestVersion(version int64) error {
	var ts [VersionSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(version))
	return db.storage.Set([]byte(latestVersionKey), ts[:], defaultWriteOpts)
}

func (db *Database) GetLatestVersion() (int64, error) {
	bz, closer, err := db.storage.Get([]byte(latestVersionKey))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			// in case of a fresh database
			return 0, nil
		}

		return 0, err
	}

	if len(bz) == 0 {
		return 0, closer.Close()
	}

	return int64(binary.LittleEndian.Uint64(bz)), closer.Close()
}

func (db *Database) Has(storeKey string, version int64, key []byte) (bool, error) {
	val, err := db.Get(storeKey, version, key)
	if err != nil {
		return false, err
	}

	return val != nil, nil
}

func (db *Database) Get(storeKey string, targetVersion int64, key []byte) ([]byte, error) {
	prefixedVal, err := getMVCCSlice(db.storage, storeKey, key, targetVersion)
	if err != nil {
		if errors.Is(err, utils.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to perform PebbleDB read: %w", err)
	}

	valBz, tombBz, ok := SplitMVCCKey(prefixedVal)
	if !ok {
		return nil, fmt.Errorf("invalid PebbleDB MVCC value: %s", prefixedVal)
	}

	// A tombstone of zero or a target version that is less than the tombstone
	// version means the key is not deleted at the target version.
	if len(tombBz) == 0 {
		return valBz, nil
	}

	tombstone, err := decodeUint64Ascending(tombBz)
	if err != nil {
		return nil, fmt.Errorf("failed to decode value tombstone: %w", err)
	}

	// A tombstone of zero or a target version that is less than the tombstone
	// version means the key is not deleted at the target version.
	if targetVersion < tombstone {
		return valBz, nil
	}

	// the value is considered deleted
	return nil, nil
}

func (db *Database) ApplyChangeset(version int64, cs *proto.NamedChangeSet) error {
	b, err := NewBatch(db.storage, version)
	if err != nil {
		return err
	}

	for _, kvPair := range cs.Changeset.Pairs {
		if kvPair.Value == nil {
			if err := b.Delete(cs.Name, kvPair.Key); err != nil {
				return err
			}
		} else {
			if err := b.Set(cs.Name, kvPair.Key, kvPair.Value); err != nil {
				return err
			}
		}
	}

	return b.Write()
}

// Prune for the PebbleDB SS backend is currently not supported. It seems the only
// reliable way to prune is to iterate over the desired domain and either manually
// tombstone or delete. Either way, the operation would be timely.
//
// See: https://github.com/cockroachdb/cockroach/blob/33623e3ee420174a4fd3226d1284b03f0e3caaac/pkg/storage/mvcc.go#L3182
func (db *Database) Prune(version int64) error {
	panic("not implemented!")
}

func (db *Database) Iterator(storeKey string, version int64, start, end []byte) (types.Iterator, error) {
	if (start != nil && len(start) == 0) || (end != nil && len(end) == 0) {
		return nil, utils.ErrKeyEmpty
	}

	if start != nil && end != nil && bytes.Compare(start, end) > 0 {
		return nil, utils.ErrStartAfterEnd
	}

	lowerBound := MVCCEncode(prependStoreKey(storeKey, start), 0)

	var upperBound []byte
	if end != nil {
		upperBound = MVCCEncode(prependStoreKey(storeKey, end), 0)
	}

	itr, err := db.storage.NewIter(&pebble.IterOptions{LowerBound: lowerBound, UpperBound: upperBound})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}

	return newPebbleDBIterator(itr, storePrefix(storeKey), start, end, version, false), nil
}

func (db *Database) ReverseIterator(storeKey string, version int64, start, end []byte) (types.Iterator, error) {
	if (start != nil && len(start) == 0) || (end != nil && len(end) == 0) {
		return nil, utils.ErrKeyEmpty
	}

	if start != nil && end != nil && bytes.Compare(start, end) > 0 {
		return nil, utils.ErrStartAfterEnd
	}

	lowerBound := MVCCEncode(prependStoreKey(storeKey, start), 0)

	var upperBound []byte
	if end != nil {
		upperBound = MVCCEncode(prependStoreKey(storeKey, end), 0)
	}

	itr, err := db.storage.NewIter(&pebble.IterOptions{LowerBound: lowerBound, UpperBound: upperBound})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}

	return newPebbleDBIterator(itr, storePrefix(storeKey), start, end, version, true), nil
}

// Import loads the initial version of the state in parallel with numWorkers goroutines
// TODO: Potentially add retries instead of panics
func (db *Database) Import(version int64, ch <-chan sstypes.ImportEntry, numWorkers int) error {
	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		batch, err := NewBatch(db.storage, version)
		if err != nil {
			panic(err)
		}

		var counter int
		for entry := range ch {
			err := batch.Set(entry.StoreKey, entry.Key, entry.Value)
			if err != nil {
				panic(err)
			}

			counter++
			if counter%ImportCommitBatchSize == 0 {
				if err := batch.Write(); err != nil {
					panic(err)
				}

				batch, err = NewBatch(db.storage, version)
				if err != nil {
					panic(err)
				}
			}
		}

		if batch.Size() > 0 {
			if err := batch.Write(); err != nil {
				panic(err)
			}
		}
	}

	wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go worker()
	}

	wg.Wait()

	return nil
}

func storePrefix(storeKey string) []byte {
	return []byte(fmt.Sprintf(StorePrefixTpl, storeKey))
}

func prependStoreKey(storeKey string, key []byte) []byte {
	if storeKey == "" {
		return key
	}
	return append(storePrefix(storeKey), key...)
}

func getMVCCSlice(db *pebble.DB, storeKey string, key []byte, version int64) ([]byte, error) {
	// end domain is exclusive, so we need to increment the version by 1
	if version < math.MaxInt64 {
		version++
	}

	itr, err := db.NewIter(&pebble.IterOptions{
		LowerBound: MVCCEncode(prependStoreKey(storeKey, key), 0),
		UpperBound: MVCCEncode(prependStoreKey(storeKey, key), version),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}
	defer func() {
		err = utils.Join(err, itr.Close())
	}()

	if !itr.Last() {
		return nil, utils.ErrRecordNotFound
	}

	_, vBz, ok := SplitMVCCKey(itr.Key())
	if !ok {
		return nil, fmt.Errorf("invalid PebbleDB MVCC key: %s", itr.Key())
	}

	keyVersion, err := decodeUint64Ascending(vBz)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key version: %w", err)
	}
	if keyVersion > version {
		return nil, fmt.Errorf("key version too large: %d", keyVersion)
	}

	return slices.Clone(itr.Value()), nil
}
