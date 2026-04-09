package keymap

import (
	"fmt"
	"sync"

	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

var _ Keymap = &memKeymap{}

type memKeymapEntry struct {
	scopedKey *types.ScopedKey
	prevKey   []byte // nil means this is the chain head (first key ever written)
}

// An in-memory keymap implementation. When a table using a memKeymap is restarted, it loads all keys from
// the segment files.  Methods on this struct are goroutine safe.
//
// - potentially high memory usage for large keymaps
// - potentially slow startup time for large keymaps
// - very fast after startup
type memKeymap struct {
	logger *slog.Logger
	data   map[string]*memKeymapEntry
	// if true, then return an error if an update would overwrite an existing key
	doubleWriteProtection bool
	lock                  sync.RWMutex
	lastKey               []byte
}

var _ BuildKeymap = NewMemKeymap

// NewMemKeymap creates a new in-memory keymap.
func NewMemKeymap(
	logger *slog.Logger,
	_ string,
	doubleWriteProtection bool,
) (kmap Keymap, requiresReload bool, err error) {

	return &memKeymap{
		logger:                logger,
		data:                  make(map[string]*memKeymapEntry),
		doubleWriteProtection: doubleWriteProtection,
	}, true, nil
}

func (m *memKeymap) Put(keys []types.ScopedKey) error {
	if len(keys) == 0 {
		return nil
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	prevKey := m.lastKey
	for _, k := range keys {
		stringKey := util.UnsafeBytesToString(k.Key)

		if m.doubleWriteProtection {
			_, ok := m.data[stringKey]
			if ok {
				return fmt.Errorf("key %s already exists", k.Key)
			}
		}

		m.data[stringKey] = &memKeymapEntry{
			scopedKey: &k,
			prevKey:   prevKey,
		}
		prevKey = k.Key
	}

	lastKeyInBatch := keys[len(keys)-1].Key
	m.lastKey = make([]byte, len(lastKeyInBatch))
	copy(m.lastKey, lastKeyInBatch)

	return nil
}

func (m *memKeymap) Get(key []byte) (address types.Address, exists bool, err error) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	entry, ok := m.data[util.UnsafeBytesToString(key)]
	if !ok {
		return types.Address{}, false, nil
	}

	return entry.scopedKey.Address, true, nil
}

func (m *memKeymap) Delete(keys []types.ScopedKey) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	for _, key := range keys {
		delete(m.data, util.UnsafeBytesToString(key.Key))
	}

	return nil
}

func (m *memKeymap) Flush() error {
	return nil
}

func (m *memKeymap) Stop() error {
	return nil
}

func (m *memKeymap) Destroy() error {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.data = nil
	m.lastKey = nil
	return nil
}

func (m *memKeymap) ReverseIterator() (KeymapReverseIterator, error) {
	if m.lastKey == nil {
		return &emptyReverseIterator{}, nil
	}
	return &memReverseIterator{
		data:    m.data,
		nextKey: m.lastKey,
	}, nil
}

type memReverseIterator struct {
	data       map[string]*memKeymapEntry
	nextKey    []byte
	currentKey string
	hasCurrent bool
}

func (it *memReverseIterator) Next() (key []byte, address types.Address, exists bool, err error) {
	if it.nextKey == nil {
		return nil, types.Address{}, false, nil
	}

	entry, ok := it.data[string(it.nextKey)]
	if !ok {
		it.nextKey = nil
		return nil, types.Address{}, false, nil
	}

	it.currentKey = string(it.nextKey)
	it.hasCurrent = true
	it.nextKey = entry.prevKey

	return entry.scopedKey.Key, entry.scopedKey.Address, true, nil
}

func (it *memReverseIterator) Delete() error {
	if !it.hasCurrent {
		return fmt.Errorf("no current entry to delete")
	}
	delete(it.data, it.currentKey)
	it.hasCurrent = false
	return nil
}

func (it *memReverseIterator) Close() error {
	return nil
}
