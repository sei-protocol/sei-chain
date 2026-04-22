package keymap

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/Layr-Labs/eigenda/litt/types"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigensdk-go/logging"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

var _ Keymap = &LevelDBKeymap{}

// LevelDBKeymap is a keymap that uses LevelDB as the underlying storage. Methods on this struct are goroutine safe.
type LevelDBKeymap struct {
	logger logging.Logger
	db     *leveldb.DB
	// if true, then return an error if an update would overwrite an existing key
	doubleWriteProtection bool
	keymapPath            string
	alive                 atomic.Bool
	// This is a "test mode only" flag. Should be true in production use cases or anywhere that data consistency
	// is critical. Unit tests write lots of little values, and syncing each one is slow, so it may be desirable
	// to set this to false in some tests.
	syncWrites bool
}

var _ BuildKeymap = NewLevelDBKeymap

// NewLevelDBKeymap creates a new LevelDBKeymap instance.
func NewLevelDBKeymap(
	logger logging.Logger,
	keymapPath string,
	doubleWriteProtection bool) (kmap Keymap, requiresReload bool, err error) {

	return newLevelDBKeymap(logger, keymapPath, doubleWriteProtection, true)
}

// NewUnsafeLevelDBKeymap creates a new LevelDBKeymap instance. It does not use sync writes. This makes it faster,
// but unsafe if data consistency is critical (i.e. production use cases).
func NewUnsafeLevelDBKeymap(
	logger logging.Logger,
	keymapPath string,
	doubleWriteProtection bool) (kmap Keymap, requiresReload bool, err error) {

	return newLevelDBKeymap(logger, keymapPath, doubleWriteProtection, false)
}

// newLevelDBKeymap creates a new LevelDBKeymap instance.
func newLevelDBKeymap(
	logger logging.Logger,
	keymapPath string,
	doubleWriteProtection bool,
	syncWrites bool) (kmap *LevelDBKeymap, requiresReload bool, err error) {

	exists, err := util.Exists(keymapPath)
	if err != nil {
		return nil, false, fmt.Errorf("error checking for keymap directory: %w", err)
	}

	if !exists {
		err = os.MkdirAll(keymapPath, 0755)
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

	return kmap, requiresReload, nil
}

func (l *LevelDBKeymap) Put(keys []*types.ScopedKey) error {

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
	for _, k := range keys {
		batch.Put(k.Key, k.Address.Serialize())
	}

	writeOptions := &opt.WriteOptions{
		Sync: l.syncWrites,
	}

	err := l.db.Write(batch, writeOptions)
	if err != nil {
		return fmt.Errorf("failed to put batch to LevelDB: %w", err)
	}
	return nil
}

func (l *LevelDBKeymap) Get(key []byte) (types.Address, bool, error) {
	addressBytes, err := l.db.Get(key, nil)
	if err != nil {
		if errors.Is(err, leveldb.ErrNotFound) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("failed to get key from LevelDB: %w", err)
	}

	address, err := types.DeserializeAddress(addressBytes)
	if err != nil {
		return 0, false, fmt.Errorf("failed to deserialize address: %w", err)
	}

	return address, true, nil
}

func (l *LevelDBKeymap) Delete(keys []*types.ScopedKey) error {
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
