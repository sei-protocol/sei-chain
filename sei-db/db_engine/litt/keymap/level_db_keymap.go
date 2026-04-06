package keymap

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync/atomic"

	"log/slog"

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
}

var _ BuildKeymap = NewLevelDBKeymap

// NewLevelDBKeymap creates a new LevelDBKeymap instance.
func NewLevelDBKeymap(
	logger *slog.Logger,
	keymapPath string,
	doubleWriteProtection bool) (kmap Keymap, requiresReload bool, err error) {

	return newLevelDBKeymap(logger, keymapPath, doubleWriteProtection, true)
}

// NewUnsafeLevelDBKeymap creates a new LevelDBKeymap instance. It does not use sync writes. This makes it faster,
// but unsafe if data consistency is critical (i.e. production use cases).
func NewUnsafeLevelDBKeymap(
	logger *slog.Logger,
	keymapPath string,
	doubleWriteProtection bool) (kmap Keymap, requiresReload bool, err error) {

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

	return kmap, requiresReload, nil
}

func (l *LevelDBKeymap) Put(keys []*types.ScopedKey) error {

	if l.doubleWriteProtection {
		for _, k := range keys {
			_, _, ok, err := l.Get(k.Key)
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
		data := make([]byte, types.AddressLength+4 /* value size */)
		serializedAddress := k.Address.Serialize()
		copy(data[:types.AddressLength], serializedAddress)
		binary.BigEndian.PutUint32(data[types.AddressLength:], k.ValueSize)

		batch.Put(k.Key, data)
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

func (l *LevelDBKeymap) Get(key []byte) (address types.Address, length uint32, exists bool, err error) {
	rawData, err := l.db.Get(key, nil)
	if err != nil {
		if errors.Is(err, leveldb.ErrNotFound) {
			return types.Address{}, 0, false, nil
		}
		return types.Address{}, 0, false, fmt.Errorf("failed to get key from LevelDB: %w", err)
	}

	if len(rawData) != types.AddressLength+4 {
		return types.Address{}, 0, false, fmt.Errorf("invalid data length: %d", len(rawData))
	}

	address, err = types.DeserializeAddress(rawData[:types.AddressLength])
	if err != nil {
		return types.Address{}, 0, false, fmt.Errorf("failed to deserialize address: %w", err)
	}

	valueSize := binary.BigEndian.Uint32(rawData[types.AddressLength:])

	return address, valueSize, true, nil
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
