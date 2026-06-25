package keymap

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/bloom"
	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

var _ Keymap = &PebbleDBKeymap{}

// PebbleDBKeymap is a keymap that uses PebbleDB as the underlying storage. Methods on this struct are goroutine safe.
type PebbleDBKeymap struct {
	logger *slog.Logger
	db     *pebble.DB
	// if true, then return an error if an update would overwrite an existing key
	doubleWriteProtection bool
	keymapPath            string
	alive                 atomic.Bool
	// This is a "test mode only" flag. Should be true in production use cases or anywhere that data consistency
	// is critical. Unit tests write lots of little values, and syncing each one is slow, so it may be desirable
	// to set this to false in some tests.
	syncWrites bool
}

var _ BuildKeymap = NewPebbleDBKeymap

// NewPebbleDBKeymap creates a new PebbleDBKeymap instance.
func NewPebbleDBKeymap(
	logger *slog.Logger,
	keymapPath string,
	doubleWriteProtection bool) (kmap Keymap, requiresReload bool, err error) {

	return newPebbleDBKeymap(logger, keymapPath, doubleWriteProtection, true)
}

// NewUnsafePebbleDBKeymap creates a new PebbleDBKeymap instance. It does not use sync writes. This makes it faster,
// but unsafe if data consistency is critical (i.e. production use cases).
func NewUnsafePebbleDBKeymap(
	logger *slog.Logger,
	keymapPath string,
	doubleWriteProtection bool) (kmap Keymap, requiresReload bool, err error) {

	return newPebbleDBKeymap(logger, keymapPath, doubleWriteProtection, false)
}

// newPebbleDBKeymap creates a new PebbleDBKeymap instance.
func newPebbleDBKeymap(
	logger *slog.Logger,
	keymapPath string,
	doubleWriteProtection bool,
	syncWrites bool) (kmap *PebbleDBKeymap, requiresReload bool, err error) {

	exists, err := util.Exists(keymapPath)
	if err != nil {
		return nil, false, fmt.Errorf("error checking for keymap directory: %w", err)
	}

	if !exists {
		err = os.MkdirAll(keymapPath, 0750)
		if err != nil {
			return nil, false, fmt.Errorf("error creating keymap directory: %w", err)
		}
	}
	requiresReload = !exists

	db, err := pebble.Open(keymapPath, keymapPebbleOptions())
	if err != nil {
		return nil, false, fmt.Errorf("failed to open PebbleDB: %w", err)
	}

	kmap = &PebbleDBKeymap{
		logger:                logger,
		db:                    db,
		keymapPath:            keymapPath,
		doubleWriteProtection: doubleWriteProtection,
		syncWrites:            syncWrites,
	}
	kmap.alive.Store(true)

	return kmap, requiresReload, nil
}

// keymapPebbleOptions returns pebble options sized for the keymap workload:
// a sustained stream of small random keys (e.g. 32-byte hashes) written in
// batches and read back as point lookups. Stock pebble options (4MB
// memtable, single compaction, no filters) collapse under high key rates —
// the memtable rotates every second and L0 backs up, stalling every writer.
func keymapPebbleOptions() *pebble.Options {
	opts := &pebble.Options{
		MemTableSize:                64 * unit.MB,
		MemTableStopWritesThreshold: 4,
		L0CompactionThreshold:       4,
		L0StopWritesThreshold:       1000,
		LBaseMaxBytes:               64 * unit.MB,
	}
	opts.CompactionConcurrencyRange = func() (lower, upper int) { return 1, 8 }
	opts.EnsureDefaults()
	// Per-level bloom filters: keymap reads are point lookups by key.
	for i := range opts.Levels {
		opts.Levels[i].FilterPolicy = bloom.FilterPolicy(10)
		opts.Levels[i].FilterType = pebble.TableFilter
	}
	return opts
}

func (p *PebbleDBKeymap) writeOptions() *pebble.WriteOptions {
	if p.syncWrites {
		return pebble.Sync
	}
	return pebble.NoSync
}

func (p *PebbleDBKeymap) Put(keys []*types.ScopedKey) error {

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
	for _, k := range keys {
		if err := batch.Set(k.Key, k.Address.Serialize(), nil); err != nil {
			_ = batch.Close()
			return fmt.Errorf("failed to add put to batch: %w", err)
		}
	}

	err := p.db.Apply(batch, p.writeOptions())
	if err != nil {
		_ = batch.Close()
		return fmt.Errorf("failed to put batch to PebbleDB: %w", err)
	}
	if err := batch.Close(); err != nil {
		return fmt.Errorf("failed to close PebbleDB batch: %w", err)
	}
	return nil
}

func (p *PebbleDBKeymap) Get(key []byte) (types.Address, bool, error) {
	val, closer, err := p.db.Get(key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return types.Address{}, false, nil
		}
		return types.Address{}, false, fmt.Errorf("failed to get key from PebbleDB: %w", err)
	}
	// Clone the bytes before closing, since the slice is only valid until closer.Close().
	cloned := bytes.Clone(val)
	if cerr := closer.Close(); cerr != nil {
		return types.Address{}, false, fmt.Errorf("failed to close PebbleDB get closer: %w", cerr)
	}

	address, err := types.DeserializeAddress(cloned)
	if err != nil {
		return types.Address{}, false, fmt.Errorf("failed to deserialize address: %w", err)
	}

	return address, true, nil
}

func (p *PebbleDBKeymap) Delete(keys []*types.ScopedKey) error {
	batch := p.db.NewBatch()
	for _, key := range keys {
		if err := batch.Delete(key.Key, nil); err != nil {
			_ = batch.Close()
			return fmt.Errorf("failed to add delete to batch: %w", err)
		}
	}

	err := p.db.Apply(batch, p.writeOptions())
	if err != nil {
		_ = batch.Close()
		return fmt.Errorf("failed to delete keys from PebbleDB: %w", err)
	}
	if err := batch.Close(); err != nil {
		return fmt.Errorf("failed to close PebbleDB batch: %w", err)
	}

	return nil
}

func (p *PebbleDBKeymap) Stop() error {
	alive := p.alive.Swap(false)
	if !alive {
		return nil
	}

	err := p.db.Close()
	if err != nil {
		return fmt.Errorf("failed to close PebbleDB: %w", err)
	}
	return nil
}

func (p *PebbleDBKeymap) Destroy() error {
	err := p.Stop()
	if err != nil {
		return fmt.Errorf("failed to stop PebbleDB: %w", err)
	}

	p.logger.Info("deleting PebbleDB keymap", "keymap", p.keymapPath)
	err = os.RemoveAll(p.keymapPath)
	if err != nil {
		return fmt.Errorf("failed to remove PebbleDB data directory: %w", err)
	}

	return nil
}
