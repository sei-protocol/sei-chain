package keymap

import (
	"fmt"
	"os"
	"sync/atomic"

	"log/slog"

	"github.com/cockroachdb/pebble/v2"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

var _ Keymap = &PebbleKeymap{}

// PebbleKeymap is a keymap that uses PebbleDB as the underlying storage. Methods on this struct are goroutine safe.
type PebbleKeymap struct {
	logger *slog.Logger
	db     *pebble.DB
	// if true, then return an error if an update would overwrite an existing key
	doubleWriteProtection bool
	keymapPath            string
	alive                 atomic.Bool
	syncWrites            bool
}

var _ BuildKeymap = NewPebbleKeymap

// NewPebbleKeymap creates a new PebbleKeymap instance with sync writes enabled.
func NewPebbleKeymap(
	logger *slog.Logger,
	keymapPath string,
	doubleWriteProtection bool,
) (kmap Keymap, requiresReload bool, err error) {
	return newPebbleKeymap(logger, keymapPath, doubleWriteProtection, true)
}

// NewUnsafePebbleKeymap creates a new PebbleKeymap instance without sync writes. This makes it faster,
// but unsafe if data consistency is critical (i.e. production use cases).
func NewUnsafePebbleKeymap(
	logger *slog.Logger,
	keymapPath string,
	doubleWriteProtection bool,
) (kmap Keymap, requiresReload bool, err error) {
	return newPebbleKeymap(logger, keymapPath, doubleWriteProtection, false)
}

func newPebbleKeymap(
	logger *slog.Logger,
	keymapPath string,
	doubleWriteProtection bool,
	syncWrites bool,
) (kmap *PebbleKeymap, requiresReload bool, err error) {

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

	opts := &pebble.Options{
		FormatMajorVersion: pebble.FormatVirtualSSTables,
	}
	if !syncWrites {
		opts.DisableWAL = true
	}

	db, err := pebble.Open(keymapPath, opts)
	if err != nil {
		return nil, false, fmt.Errorf("failed to open PebbleDB: %w", err)
	}

	kmap = &PebbleKeymap{
		logger:                logger,
		db:                    db,
		keymapPath:            keymapPath,
		doubleWriteProtection: doubleWriteProtection,
		syncWrites:            syncWrites,
	}
	kmap.alive.Store(true)

	return kmap, requiresReload, nil
}

func (p *PebbleKeymap) Put(keys []*types.ScopedKey) error {
	if p.doubleWriteProtection {
		for _, k := range keys {
			_, ok, err := p.Get(k.Key)
			if err != nil {
				return fmt.Errorf("failed to get key: %w", err)
			}
			if ok {
				return fmt.Errorf("key %s already exists", k.Key)
			}
		}
	}

	batch := p.db.NewBatch()
	defer func() { _ = batch.Close() }()

	var addrBuf [types.AddressLength]byte
	for _, k := range keys {
		k.Address.SerializeInto(addrBuf[:])
		if err := batch.Set(k.Key, addrBuf[:], nil); err != nil {
			return fmt.Errorf("failed to set key in PebbleDB batch: %w", err)
		}
	}

	writeOpts := pebble.NoSync
	if p.syncWrites {
		writeOpts = pebble.Sync
	}

	if err := batch.Commit(writeOpts); err != nil {
		return fmt.Errorf("failed to commit batch to PebbleDB: %w", err)
	}
	return nil
}

func (p *PebbleKeymap) Get(key []byte) (address types.Address, exists bool, err error) {
	rawData, closer, err := p.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return types.Address{}, false, nil
		}
		return types.Address{}, false, fmt.Errorf("failed to get key from PebbleDB: %w", err)
	}
	defer func() { _ = closer.Close() }()

	if len(rawData) != types.AddressLength {
		return types.Address{}, false, fmt.Errorf("invalid data length: %d", len(rawData))
	}

	address, err = types.DeserializeAddress(rawData[:types.AddressLength])
	if err != nil {
		return types.Address{}, false, fmt.Errorf("failed to deserialize address: %w", err)
	}

	return address, true, nil
}

func (p *PebbleKeymap) Delete(keys []*types.ScopedKey) error {
	batch := p.db.NewBatch()
	defer func() { _ = batch.Close() }()

	for _, key := range keys {
		if err := batch.Delete(key.Key, nil); err != nil {
			return fmt.Errorf("failed to delete key in PebbleDB batch: %w", err)
		}
	}

	if err := batch.Commit(pebble.NoSync); err != nil {
		return fmt.Errorf("failed to commit delete batch to PebbleDB: %w", err)
	}

	return nil
}

func (p *PebbleKeymap) Stop() error {
	alive := p.alive.Swap(false)
	if !alive {
		return nil
	}

	if err := p.db.Close(); err != nil {
		return fmt.Errorf("failed to close PebbleDB: %w", err)
	}
	return nil
}

func (p *PebbleKeymap) Destroy() error {
	if err := p.Stop(); err != nil {
		return fmt.Errorf("failed to stop PebbleDB: %w", err)
	}

	p.logger.Info(fmt.Sprintf("deleting PebbleDB keymap at path: %s", p.keymapPath))
	if err := os.RemoveAll(p.keymapPath); err != nil {
		return fmt.Errorf("failed to remove PebbleDB data directory: %w", err)
	}

	return nil
}
