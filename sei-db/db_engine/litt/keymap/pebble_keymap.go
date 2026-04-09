package keymap

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"log/slog"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/bloom"
	"github.com/cockroachdb/pebble/v2/sstable"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/metrics"
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

	m *metrics.LittDBMetrics

	// writeLock serializes Put calls to maintain linked-list consistency across batches.
	writeLock sync.Mutex
	lastKey   []byte
	// Reusable buffer for encodeLinkedValue, safe because Put is serialized by writeLock.
	encodeBuf []byte
}

var _ BuildKeymap = NewPebbleKeymap

// NewPebbleKeymap creates a new PebbleKeymap instance with sync writes enabled.
func NewPebbleKeymap(
	logger *slog.Logger,
	keymapPath string,
	doubleWriteProtection bool,
	m *metrics.LittDBMetrics,
) (kmap Keymap, requiresReload bool, err error) {
	return newPebbleKeymap(logger, keymapPath, doubleWriteProtection, true, m)
}

// NewUnsafePebbleKeymap creates a new PebbleKeymap instance without sync writes. This makes it faster,
// but unsafe if data consistency is critical (i.e. production use cases).
func NewUnsafePebbleKeymap(
	logger *slog.Logger,
	keymapPath string,
	doubleWriteProtection bool,
	m *metrics.LittDBMetrics,
) (kmap Keymap, requiresReload bool, err error) {
	return newPebbleKeymap(logger, keymapPath, doubleWriteProtection, false, m)
}

func newPebbleKeymap(
	logger *slog.Logger,
	keymapPath string,
	doubleWriteProtection bool,
	syncWrites bool,
	m *metrics.LittDBMetrics,
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

	cache := pebble.NewCache(32 << 20) // 32 MiB
	defer cache.Unref()

	opts := &pebble.Options{
		Cache:                       cache,
		CompactionConcurrencyRange:  func() (int, int) { return 2, 4 },
		FormatMajorVersion:          pebble.FormatVirtualSSTables,
		L0CompactionThreshold:       8,
		L0StopWritesThreshold:       1000,
		LBaseMaxBytes:               64 << 20,
		MemTableSize:                64 << 20,
		MemTableStopWritesThreshold: 4,
	}
	if !syncWrites {
		opts.DisableWAL = true
	}

	opts.Levels[0].BlockSize = 32 << 10
	opts.Levels[0].IndexBlockSize = 256 << 10
	opts.Levels[0].FilterPolicy = bloom.FilterPolicy(10)
	opts.Levels[0].FilterType = pebble.TableFilter
	opts.Levels[0].Compression = func() *sstable.CompressionProfile { return sstable.NoCompression }
	opts.Levels[0].EnsureL0Defaults()

	for i := 1; i < len(opts.Levels); i++ {
		l := &opts.Levels[i]
		l.BlockSize = 32 << 10
		l.IndexBlockSize = 256 << 10
		l.FilterPolicy = bloom.FilterPolicy(10)
		l.FilterType = pebble.TableFilter
		l.Compression = func() *sstable.CompressionProfile { return sstable.NoCompression }
		l.EnsureL1PlusDefaults(&opts.Levels[i-1])
	}
	opts.Levels[6].FilterPolicy = nil

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
		m:                     m,
	}
	kmap.alive.Store(true)

	rawData, closer, getErr := db.Get(latestKeyMetaKey)
	if getErr == nil {
		kmap.lastKey = make([]byte, len(rawData))
		copy(kmap.lastKey, rawData)
		_ = closer.Close()
	} else if getErr != pebble.ErrNotFound {
		_ = db.Close()
		return nil, false, fmt.Errorf("failed to read latest key metadata: %w", getErr)
	}

	return kmap, requiresReload, nil
}

func (p *PebbleKeymap) Put(keys []types.ScopedKey) error {
	if len(keys) == 0 {
		return nil
	}

	p.m.SetKeymapManagerPhase("put/lock")
	p.writeLock.Lock()
	defer p.writeLock.Unlock()

	if p.doubleWriteProtection {
		p.m.SetKeymapManagerPhase("put/double_write_check")
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

	p.m.SetKeymapManagerPhase("put/build_batch")
	batch := p.db.NewBatch()
	defer func() { _ = batch.Close() }()

	prevKey := p.lastKey
	for _, k := range keys {
		val := p.encodeLinkedValuePooled(k.Address, prevKey)
		if err := batch.Set(k.Key, val, nil); err != nil {
			return fmt.Errorf("failed to set key in PebbleDB batch: %w", err)
		}
		prevKey = k.Key
	}

	lastKeyInBatch := keys[len(keys)-1].Key
	if err := batch.Set(latestKeyMetaKey, lastKeyInBatch, nil); err != nil {
		return fmt.Errorf("failed to set latest key metadata: %w", err)
	}

	p.m.SetKeymapManagerPhase("put/commit")
	if err := batch.Commit(pebble.NoSync); err != nil {
		return fmt.Errorf("failed to commit batch to PebbleDB: %w", err)
	}

	p.lastKey = make([]byte, len(lastKeyInBatch))
	copy(p.lastKey, lastKeyInBatch)

	return nil
}

// encodeLinkedValuePooled encodes into p.encodeBuf, growing it as needed.
// The returned slice is valid only until the next call. Caller must hold writeLock.
func (p *PebbleKeymap) encodeLinkedValuePooled(address types.Address, prevKey []byte) []byte {
	size := types.AddressLength + prevKeyLenSize + len(prevKey)
	if cap(p.encodeBuf) < size {
		p.encodeBuf = make([]byte, size)
	}
	buf := p.encodeBuf[:size]
	address.SerializeInto(buf[:types.AddressLength])
	binary.BigEndian.PutUint32(buf[types.AddressLength:types.AddressLength+prevKeyLenSize], uint32(len(prevKey))) //nolint:gosec
	if len(prevKey) > 0 {
		copy(buf[types.AddressLength+prevKeyLenSize:], prevKey)
	}
	return buf
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

	address, _, err = decodeLinkedValue(rawData)
	if err != nil {
		return types.Address{}, false, fmt.Errorf("failed to decode value from PebbleDB: %w", err)
	}

	return address, true, nil
}

func (p *PebbleKeymap) Delete(keys []types.ScopedKey) error {
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

func (p *PebbleKeymap) Flush() error {
	if !p.syncWrites {
		return nil
	}
	batch := p.db.NewBatch()
	defer func() { _ = batch.Close() }()
	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to sync PebbleDB WAL: %w", err)
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

func (p *PebbleKeymap) ReverseIterator() (KeymapReverseIterator, error) {
	rawData, closer, err := p.db.Get(latestKeyMetaKey)
	if err != nil {
		if err == pebble.ErrNotFound {
			return &emptyReverseIterator{}, nil
		}
		return nil, fmt.Errorf("failed to read latest key metadata: %w", err)
	}
	headKey := make([]byte, len(rawData))
	copy(headKey, rawData)
	_ = closer.Close()

	return &pebbleReverseIterator{
		db:      p.db,
		nextKey: headKey,
	}, nil
}

type pebbleReverseIterator struct {
	db         *pebble.DB
	nextKey    []byte
	currentKey []byte
}

func (it *pebbleReverseIterator) Next() (key []byte, address types.Address, exists bool, err error) {
	if it.nextKey == nil {
		return nil, types.Address{}, false, nil
	}

	rawData, closer, err := it.db.Get(it.nextKey)
	if err != nil {
		if err == pebble.ErrNotFound {
			it.nextKey = nil
			return nil, types.Address{}, false, nil
		}
		return nil, types.Address{}, false, fmt.Errorf("failed to get key from PebbleDB: %w", err)
	}

	address, prevKey, err := decodeLinkedValue(rawData)
	_ = closer.Close()
	if err != nil {
		return nil, types.Address{}, false, fmt.Errorf("failed to decode linked value: %w", err)
	}

	it.currentKey = it.nextKey
	it.nextKey = prevKey

	return it.currentKey, address, true, nil
}

func (it *pebbleReverseIterator) Delete() error {
	if it.currentKey == nil {
		return fmt.Errorf("no current entry to delete")
	}
	return it.db.Delete(it.currentKey, pebble.NoSync)
}

func (it *pebbleReverseIterator) Close() error {
	return nil
}
