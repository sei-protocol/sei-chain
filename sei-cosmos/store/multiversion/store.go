package multiversion

import (
	"sync"
)

type MultiVersionStore interface {
	GetLatest(key []byte) (value MultiVersionValueItem)
	GetLatestBeforeIndex(index int, key []byte) (value MultiVersionValueItem)
	Set(index int, incarnation int, key []byte, value []byte)
	SetEstimate(index int, incarnation int, key []byte)
	Delete(index int, incarnation int, key []byte)
	Has(index int, key []byte) bool
	// TODO: do we want to add helper functions for validations with readsets / applying writesets ?
}

type Store struct {
	mtx sync.RWMutex
	// map that stores the key -> MultiVersionValue mapping for accessing from a given key
	multiVersionMap map[string]MultiVersionValue
	// TODO: do we need to add something here to persist readsets for later validation
	// TODO: we need to support iterators as well similar to how cachekv does it
	// TODO: do we need secondary indexing on index -> keys - this way if we need to abort we can replace those keys with ESTIMATE values? - maybe this just means storing writeset
}

func NewMultiVersionStore() *Store {
	return &Store{
		multiVersionMap: make(map[string]MultiVersionValue),
	}
}

// GetLatest implements MultiVersionStore.
func (s *Store) GetLatest(key []byte) (value MultiVersionValueItem) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	keyString := string(key)
	// if the key doesn't exist in the overall map, return nil
	if _, ok := s.multiVersionMap[keyString]; !ok {
		return nil
	}
	val, found := s.multiVersionMap[keyString].GetLatest()
	if !found {
		return nil // this shouldn't be possible
	}
	return val
}

// GetLatestBeforeIndex implements MultiVersionStore.
func (s *Store) GetLatestBeforeIndex(index int, key []byte) (value MultiVersionValueItem) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	keyString := string(key)
	// if the key doesn't exist in the overall map, return nil
	if _, ok := s.multiVersionMap[keyString]; !ok {
		return nil
	}
	val, found := s.multiVersionMap[keyString].GetLatestBeforeIndex(index)
	// otherwise, we may have found a value for that key, but its not written before the index passed in
	if !found {
		return nil
	}
	// found a value prior to the passed in index, return that value (could be estimate OR deleted, but it is a definitive value)
	return val
}

// Has implements MultiVersionStore. It checks if the key exists in the multiversion store at or before the specified index.
func (s *Store) Has(index int, key []byte) bool {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	keyString := string(key)
	if _, ok := s.multiVersionMap[keyString]; !ok {
		return false // this is okay because the caller of this will THEN need to access the parent store to verify that the key doesnt exist there
	}
	_, found := s.multiVersionMap[keyString].GetLatestBeforeIndex(index)
	return found
}

// This function will try to intialize the multiversion item if it doesn't exist for a key specified by byte array
// NOTE: this should be used within an acquired mutex lock
func (s *Store) tryInitMultiVersionItem(keyString string) {
	if _, ok := s.multiVersionMap[keyString]; !ok {
		multiVersionValue := NewMultiVersionItem()
		s.multiVersionMap[keyString] = multiVersionValue
	}
}

// Set implements MultiVersionStore.
func (s *Store) Set(index int, incarnation int, key []byte, value []byte) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	keyString := string(key)
	s.tryInitMultiVersionItem(keyString)
	s.multiVersionMap[keyString].Set(index, incarnation, value)
}

// SetEstimate implements MultiVersionStore.
func (s *Store) SetEstimate(index int, incarnation int, key []byte) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	keyString := string(key)
	s.tryInitMultiVersionItem(keyString)
	s.multiVersionMap[keyString].SetEstimate(index, incarnation)
}

// Delete implements MultiVersionStore.
func (s *Store) Delete(index int, incarnation int, key []byte) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	keyString := string(key)
	s.tryInitMultiVersionItem(keyString)
	s.multiVersionMap[keyString].Delete(index, incarnation)
}

var _ MultiVersionStore = (*Store)(nil)
