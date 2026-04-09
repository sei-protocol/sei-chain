package keymap

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

var _ Keymap = &LevelDBKeymap{}

// LevelDBKeymap is a keymap that uses LevelDB as the underlying storage. Methods on this struct are goroutine safe.
type LevelDBKeymap struct {
	logger *slog.Logger
	db     *leveldb.DB
	// if true, then return an error if an update would overwrite an existing key
	doubleWriteProtection bool
	keymapPath            string
	alive                 atomic.Bool
	// This is a "test mode only" flag. Should be true in production use cases or anywhere that data consistency
	// is critical. Unit tests write lots of little values, and syncing each one is slow, so it may be desirable
	// to set this to false in some tests.
	syncWrites bool

	// writeLock serializes Put calls to maintain linked-list consistency across batches.
	writeLock sync.Mutex
	lastKey   []byte
}

var _ BuildKeymap = NewLevelDBKeymap

// NewLevelDBKeymap creates a new LevelDBKeymap instance.
func NewLevelDBKeymap(
	logger *slog.Logger,
	keymapPath string,
	doubleWriteProtection bool,
	_ *metrics.LittDBMetrics,
) (kmap Keymap, requiresReload bool, err error) {
	return newLevelDBKeymap(logger, keymapPath, doubleWriteProtection, true)
}

// NewUnsafeLevelDBKeymap creates a new LevelDBKeymap instance. It does not use sync writes. This makes it faster,
// but unsafe if data consistency is critical (i.e. production use cases).
func NewUnsafeLevelDBKeymap(
	logger *slog.Logger,
	keymapPath string,
	doubleWriteProtection bool,
	_ *metrics.LittDBMetrics,
) (kmap Keymap, requiresReload bool, err error) {
	return newLevelDBKeymap(logger, keymapPath, doubleWriteProtection, false)
}

// newLevelDBKeymap creates a new LevelDBKeymap instance.
func newLevelDBKeymap(
	logger *slog.Logger,
	keymapPath string,
	doubleWriteProtection bool,
	syncWrites bool) (kmap *LevelDBKeymap, requiresReload bool, err error) {

	exists, err := util.Exists(keymapPath)
	if err != nil {
		return nil, false, fmt.Errorf("error checking for keymap directory: %w", err)
	}

	if !exists {
		err = os.MkdirAll(keymapPath, 0755) //nolint:gosec
		if err != nil {
			return nil, false, fmt.Errorf("error creating keymap directory: %w", err)
		}
	}
	requiresReload = !exists

	db, err := leveldb.OpenFile(keymapPath, nil)
	if err != nil {
		return nil, false, fmt.Errorf("failed to open LevelDB: %w", err)
	}

	kmap = &LevelDBKeymap{
		logger:                logger,
		db:                    db,
		keymapPath:            keymapPath,
		doubleWriteProtection: doubleWriteProtection,
		syncWrites:            syncWrites,
	}
	kmap.alive.Store(true)

	rawData, getErr := db.Get(latestKeyMetaKey, nil)
	if getErr == nil {
		kmap.lastKey = make([]byte, len(rawData))
		copy(kmap.lastKey, rawData)
	} else if !errors.Is(getErr, leveldb.ErrNotFound) {
		_ = db.Close()
		return nil, false, fmt.Errorf("failed to read latest key metadata: %w", getErr)
	}

	return kmap, requiresReload, nil
}

func (l *LevelDBKeymap) Put(keys []types.ScopedKey) error {
	if len(keys) == 0 {
		return nil
	}

	l.writeLock.Lock()
	defer l.writeLock.Unlock()

	if l.doubleWriteProtection {
		for _, k := range keys {
			_, ok, err := l.Get(k.Key)
			if err != nil {
				return fmt.Errorf("failed to get key: %w", err)
			}
			if ok {
				return fmt.Errorf("key %s already exists", k.Key)
			}
		}
	}

	batch := new(leveldb.Batch)
	prevKey := l.lastKey
	for _, k := range keys {
		val := encodeLinkedValue(k.Address, prevKey)
		batch.Put(k.Key, val)
		prevKey = k.Key
	}

	lastKeyInBatch := keys[len(keys)-1].Key
	batch.Put(latestKeyMetaKey, lastKeyInBatch)

	writeOptions := &opt.WriteOptions{
		Sync: l.syncWrites,
	}

	err := l.db.Write(batch, writeOptions)
	if err != nil {
		return fmt.Errorf("failed to put batch to LevelDB: %w", err)
	}

	l.lastKey = make([]byte, len(lastKeyInBatch))
	copy(l.lastKey, lastKeyInBatch)

	return nil
}

func (l *LevelDBKeymap) Get(key []byte) (address types.Address, exists bool, err error) {
	rawData, err := l.db.Get(key, nil)
	if err != nil {
		if errors.Is(err, leveldb.ErrNotFound) {
			return types.Address{}, false, nil
		}
		return types.Address{}, false, fmt.Errorf("failed to get key from LevelDB: %w", err)
	}

	address, _, err = decodeLinkedValue(rawData)
	if err != nil {
		return types.Address{}, false, fmt.Errorf("failed to decode value from LevelDB: %w", err)
	}

	return address, true, nil
}

func (l *LevelDBKeymap) Delete(keys []types.ScopedKey) error {
	batch := new(leveldb.Batch)
	for _, key := range keys {
		batch.Delete(key.Key)
	}

	err := l.db.Write(batch, nil)
	if err != nil {
		return fmt.Errorf("failed to delete keys from LevelDB: %w", err)
	}

	return nil
}

func (l *LevelDBKeymap) Flush() error {
	return nil
}

func (l *LevelDBKeymap) Stop() error {
	alive := l.alive.Swap(false)
	if !alive {
		return nil
	}

	err := l.db.Close()
	if err != nil {
		return fmt.Errorf("failed to close LevelDB: %w", err)
	}
	return nil
}

func (l *LevelDBKeymap) Destroy() error {
	err := l.Stop()
	if err != nil {
		return fmt.Errorf("failed to stop LevelDB: %w", err)
	}

	l.logger.Info(fmt.Sprintf("deleting LevelDB keymap at path: %s", l.keymapPath))
	err = os.RemoveAll(l.keymapPath)
	if err != nil {
		return fmt.Errorf("failed to remove LevelDB data directory: %w", err)
	}

	return nil
}

func (l *LevelDBKeymap) ReverseIterator() (KeymapReverseIterator, error) {
	rawData, err := l.db.Get(latestKeyMetaKey, nil)
	if err != nil {
		if errors.Is(err, leveldb.ErrNotFound) {
			return &emptyReverseIterator{}, nil
		}
		return nil, fmt.Errorf("failed to read latest key metadata: %w", err)
	}
	headKey := make([]byte, len(rawData))
	copy(headKey, rawData)

	return &leveldbReverseIterator{
		db:      l.db,
		nextKey: headKey,
	}, nil
}

type leveldbReverseIterator struct {
	db         *leveldb.DB
	nextKey    []byte
	currentKey []byte
}

func (it *leveldbReverseIterator) Next() (key []byte, address types.Address, exists bool, err error) {
	if it.nextKey == nil {
		return nil, types.Address{}, false, nil
	}

	rawData, err := it.db.Get(it.nextKey, nil)
	if err != nil {
		if errors.Is(err, leveldb.ErrNotFound) {
			it.nextKey = nil
			return nil, types.Address{}, false, nil
		}
		return nil, types.Address{}, false, fmt.Errorf("failed to get key from LevelDB: %w", err)
	}

	address, prevKey, err := decodeLinkedValue(rawData)
	if err != nil {
		return nil, types.Address{}, false, fmt.Errorf("failed to decode linked value: %w", err)
	}

	it.currentKey = it.nextKey
	it.nextKey = prevKey

	return it.currentKey, address, true, nil
}

func (it *leveldbReverseIterator) Delete() error {
	if it.currentKey == nil {
		return fmt.Errorf("no current entry to delete")
	}
	return it.db.Delete(it.currentKey, nil)
}

func (it *leveldbReverseIterator) Close() error {
	return nil
}
