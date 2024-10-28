package app

import (
	"errors"
	"sort"
	"sync"

	seidbproto "github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/ss/types"
)

// InMemoryStateStore this implements seidb state store with an inmemory store
type InMemoryStateStore struct {
	mu              sync.RWMutex
	data            map[string]map[int64]map[string][]byte
	latestVersion   int64
	earliestVersion int64
}

func (s *InMemoryStateStore) GetLatestMigratedKey() ([]byte, error) {
	// TODO: Add get call here
	return nil, nil
}

func (s *InMemoryStateStore) SetLatestMigratedKey(key []byte) error {
	// TODO: Add set call here
	return nil
}

func (s *InMemoryStateStore) GetLatestMigratedModule() (string, error) {
	// TODO: Add get call here
	return "", nil
}

func (s *InMemoryStateStore) SetLatestMigratedModule(module string) error {
	// TODO: Add set call here
	return nil
}

func NewInMemoryStateStore() *InMemoryStateStore {
	return &InMemoryStateStore{
		data:            make(map[string]map[int64]map[string][]byte),
		latestVersion:   0,
		earliestVersion: 0,
	}
}

func (s *InMemoryStateStore) Get(storeKey string, version int64, key []byte) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	store, ok := s.data[storeKey]
	if !ok {
		return nil, errors.New("not found")
	}

	versionData, ok := store[version]
	if !ok {
		return nil, errors.New("not found")
	}

	value, ok := versionData[string(key)]
	if !ok {
		return nil, errors.New("not found")
	}

	return value, nil
}

func (s *InMemoryStateStore) Has(storeKey string, version int64, key []byte) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	store, ok := s.data[storeKey]
	if !ok {
		return false, nil
	}

	versionData, ok := store[version]
	if !ok {
		return false, nil
	}

	_, ok = versionData[string(key)]
	return ok, nil
}

func (s *InMemoryStateStore) Iterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	store, ok := s.data[storeKey]
	if !ok {
		return nil, errors.New("store not found")
	}

	versionData, ok := store[version]
	if !ok {
		return nil, errors.New("version not found")
	}

	return NewInMemoryIterator(versionData, start, end), nil
}

func (s *InMemoryStateStore) ReverseIterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	store, ok := s.data[storeKey]
	if !ok {
		return nil, errors.New("store not found")
	}

	versionData, ok := store[version]
	if !ok {
		return nil, errors.New("version not found")
	}

	iter := NewInMemoryIterator(versionData, start, end)

	// Reverse the keys for reverse iteration
	for i, j := 0, len(iter.keys)-1; i < j; i, j = i+1, j-1 {
		iter.keys[i], iter.keys[j] = iter.keys[j], iter.keys[i]
	}

	return iter, nil
}

func (s *InMemoryStateStore) RawIterate(storeKey string, fn func([]byte, []byte, int64) bool) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for version, versionData := range s.data[storeKey] {
		for key, value := range versionData {
			if !fn([]byte(key), value, version) {
				return false, nil
			}
		}
	}

	return true, nil
}

func (s *InMemoryStateStore) GetLatestVersion() (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.latestVersion, nil
}

func (s *InMemoryStateStore) SetLatestVersion(version int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.latestVersion = version
	return nil
}

func (s *InMemoryStateStore) GetEarliestVersion() (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.earliestVersion, nil
}

func (s *InMemoryStateStore) SetEarliestVersion(version int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.earliestVersion = version
	return nil
}

func (s *InMemoryStateStore) ApplyChangeset(version int64, cs *seidbproto.NamedChangeSet) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, pair := range cs.Changeset.Pairs {
		storeKey := cs.Name
		key := pair.Key
		value := pair.Value

		if s.data[storeKey] == nil {
			s.data[storeKey] = make(map[int64]map[string][]byte)
		}

		if s.data[storeKey][version] == nil {
			s.data[storeKey][version] = make(map[string][]byte)
		}

		if pair.Delete {
			delete(s.data[storeKey][version], string(key))
		} else {
			s.data[storeKey][version][string(key)] = value
		}
	}

	s.latestVersion = version
	return nil
}

func (s *InMemoryStateStore) ApplyChangesetAsync(version int64, changesets []*seidbproto.NamedChangeSet) error {
	// Implementation for async write, currently just calls ApplyChangeset
	for _, cs := range changesets {
		if err := s.ApplyChangeset(version, cs); err != nil {
			return err
		}
	}
	return nil
}

func (s *InMemoryStateStore) Import(version int64, ch <-chan types.SnapshotNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for node := range ch {
		storeKey := node.StoreKey
		key := node.Key
		value := node.Value

		if s.data[storeKey] == nil {
			s.data[storeKey] = make(map[int64]map[string][]byte)
		}

		if s.data[storeKey][version] == nil {
			s.data[storeKey][version] = make(map[string][]byte)
		}

		s.data[storeKey][version][string(key)] = value
	}

	s.latestVersion = version
	return nil
}

func (s *InMemoryStateStore) RawImport(ch <-chan types.RawSnapshotNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var latestVersion int64

	for node := range ch {
		storeKey := node.StoreKey
		key := node.Key
		value := node.Value
		version := node.Version

		if s.data[storeKey] == nil {
			s.data[storeKey] = make(map[int64]map[string][]byte)
		}

		if s.data[storeKey][version] == nil {
			s.data[storeKey][version] = make(map[string][]byte)
		}

		s.data[storeKey][version][string(key)] = value

		if version > latestVersion {
			latestVersion = version
		}
	}

	s.latestVersion = latestVersion
	return nil
}

func (s *InMemoryStateStore) Prune(version int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for storeKey, store := range s.data {
		for ver := range store {
			if ver <= version {
				delete(store, ver)
			}
		}
		if len(store) == 0 {
			delete(s.data, storeKey)
		}
	}

	if s.earliestVersion <= version {
		s.earliestVersion = version + 1
	}

	return nil
}

func (s *InMemoryStateStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data = make(map[string]map[int64]map[string][]byte)
	return nil
}

type InMemoryIterator struct {
	data    map[string][]byte
	keys    []string
	current int
	start   []byte
	end     []byte
}

func NewInMemoryIterator(data map[string][]byte, start, end []byte) *InMemoryIterator {
	keys := make([]string, 0, len(data))
	for k := range data {
		if (start == nil || k >= string(start)) && (end == nil || k < string(end)) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	return &InMemoryIterator{
		data:    data,
		keys:    keys,
		current: 0,
		start:   start,
		end:     end,
	}
}

func (it *InMemoryIterator) Domain() (start, end []byte) {
	return it.start, it.end
}

func (it *InMemoryIterator) Valid() bool {
	return it.current >= 0 && it.current < len(it.keys)
}

func (it *InMemoryIterator) Next() {
	if it.Valid() {
		it.current++
	}
}

func (it *InMemoryIterator) Key() (key []byte) {
	if !it.Valid() {
		panic("iterator is invalid")
	}
	return []byte(it.keys[it.current])
}

func (it *InMemoryIterator) Value() (value []byte) {
	if !it.Valid() {
		panic("iterator is invalid")
	}
	return it.data[it.keys[it.current]]
}

func (it *InMemoryIterator) Error() error {
	return nil
}

func (it *InMemoryIterator) Close() error {
	it.data = nil
	it.keys = nil
	return nil
}
