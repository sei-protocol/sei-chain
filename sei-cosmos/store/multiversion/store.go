package multiversion

import (
	"sort"
	"sync"

	"github.com/cosmos/cosmos-sdk/store/types"
)

type MultiVersionStore interface {
	GetLatest(key []byte) (value MultiVersionValueItem)
	GetLatestBeforeIndex(index int, key []byte) (value MultiVersionValueItem)
	Set(index int, incarnation int, key []byte, value []byte) // TODO: maybe we don't need these if all writes are coming from writesets
	SetEstimate(index int, incarnation int, key []byte)       // TODO: maybe we don't need these if all writes are coming from writesets
	Delete(index int, incarnation int, key []byte)            // TODO: maybe we don't need these if all writes are coming from writesets
	Has(index int, key []byte) bool
	WriteLatestToStore(parentStore types.KVStore)
	SetWriteset(index int, incarnation int, writeset WriteSet)
	InvalidateWriteset(index int, incarnation int)
	SetEstimatedWriteset(index int, incarnation int, writeset WriteSet)
	GetAllWritesetKeys() map[int][]string
}

type WriteSet map[string][]byte

var _ MultiVersionStore = (*Store)(nil)

type Store struct {
	mtx sync.RWMutex
	// map that stores the key -> MultiVersionValue mapping for accessing from a given key
	multiVersionMap map[string]MultiVersionValue
	// TODO: do we need to support iterators as well similar to how cachekv does it - yes

	txWritesetKeys map[int][]string // map of tx index -> writeset keys
}

func NewMultiVersionStore() *Store {
	return &Store{
		multiVersionMap: make(map[string]MultiVersionValue),
		txWritesetKeys:  make(map[int][]string),
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
		return nil // this is possible IF there is are writeset that are then removed for that key
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

func (s *Store) removeOldWriteset(index int, newWriteSet WriteSet) {
	writeset := make(map[string][]byte)
	if newWriteSet != nil {
		// if non-nil writeset passed in, we can use that to optimize removals
		writeset = newWriteSet
	}
	// if there is already a writeset existing, we should remove that fully
	if keys, ok := s.txWritesetKeys[index]; ok {
		// we need to delete all of the keys in the writeset from the multiversion store
		for _, key := range keys {
			// small optimization to check if the new writeset is going to write this key, if so, we can leave it behind
			if _, ok := writeset[key]; ok {
				// we don't need to remove this key because it will be overwritten anyways - saves the operation of removing + rebalancing underlying btree
				continue
			}
			// remove from the appropriate item if present in multiVersionMap
			if val, ok := s.multiVersionMap[key]; ok {
				val.Remove(index)
			}
		}
	}
	// unset the writesetKeys for this index
	delete(s.txWritesetKeys, index)
}

// SetWriteset sets a writeset for a transaction index, and also writes all of the multiversion items in the writeset to the multiversion store.
func (s *Store) SetWriteset(index int, incarnation int, writeset WriteSet) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	// remove old writeset if it exists
	s.removeOldWriteset(index, writeset)

	writeSetKeys := make([]string, 0, len(writeset))
	for key, value := range writeset {
		writeSetKeys = append(writeSetKeys, key)
		s.tryInitMultiVersionItem(key)
		if value == nil {
			// delete if nil value
			s.multiVersionMap[key].Delete(index, incarnation)
		} else {
			s.multiVersionMap[key].Set(index, incarnation, value)
		}
	}
	sort.Strings(writeSetKeys)
	s.txWritesetKeys[index] = writeSetKeys
}

// InvalidateWriteset iterates over the keys for the given index and incarnation writeset and replaces with ESTIMATEs
func (s *Store) InvalidateWriteset(index int, incarnation int) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if keys, ok := s.txWritesetKeys[index]; ok {
		for _, key := range keys {
			// invalidate all of the writeset items - is this suboptimal? - we could potentially do concurrently if slow because locking is on an item specific level
			s.tryInitMultiVersionItem(key) // this SHOULD no-op because we're invalidating existing keys
			s.multiVersionMap[key].SetEstimate(index, incarnation)
		}
	}
	// we leave the writeset in place because we'll need it for key removal later if/when we replace with a new writeset
}

// SetEstimatedWriteset is used to directly write estimates instead of writing a writeset and later invalidating
func (s *Store) SetEstimatedWriteset(index int, incarnation int, writeset WriteSet) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	// remove old writeset if it exists
	s.removeOldWriteset(index, writeset)

	writeSetKeys := make([]string, 0, len(writeset))
	// still need to save the writeset so we can remove the elements later:
	for key := range writeset {
		writeSetKeys = append(writeSetKeys, key)
		s.tryInitMultiVersionItem(key)
		s.multiVersionMap[key].SetEstimate(index, incarnation)
	}
	sort.Strings(writeSetKeys)
	s.txWritesetKeys[index] = writeSetKeys
}

// GetWritesetKeys implements MultiVersionStore.
func (s *Store) GetAllWritesetKeys() map[int][]string {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.txWritesetKeys
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

func (s *Store) WriteLatestToStore(parentStore types.KVStore) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	// sort the keys
	keys := make([]string, 0, len(s.multiVersionMap))
	for key := range s.multiVersionMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		mvValue, found := s.multiVersionMap[key].GetLatestNonEstimate()
		if !found {
			// this means that at some point, there was an estimate, but we have since removed it so there isn't anything writeable at the key, so we can skip
			continue
		}
		// we shouldn't have any ESTIMATE values when performing the write, because we read the latest non-estimate values only
		if mvValue.IsEstimate() {
			panic("should not have any estimate values when writing to parent store")
		}
		// if the value is deleted, then delete it from the parent store
		if mvValue.IsDeleted() {
			// We use []byte(key) instead of conv.UnsafeStrToBytes because we cannot
			// be sure if the underlying store might do a save with the byteslice or
			// not. Once we get confirmation that .Delete is guaranteed not to
			// save the byteslice, then we can assume only a read-only copy is sufficient.
			parentStore.Delete([]byte(key))
			continue
		}
		if mvValue.Value() != nil {
			parentStore.Set([]byte(key), mvValue.Value())
		}
	}
}
