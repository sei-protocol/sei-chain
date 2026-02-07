package multiversion

import (
	"bytes"
	"sort"
	"sync"

	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/types/occ"
	occtypes "github.com/cosmos/cosmos-sdk/types/occ"
	db "github.com/tendermint/tm-db"
)

type MultiVersionStore interface {
	GetLatest(key []byte) (value MultiVersionValueItem)
	GetLatestBeforeIndex(index int, key []byte) (value MultiVersionValueItem)
	Has(index int, key []byte) bool
	WriteLatestToStore()
	SetWriteset(index int, incarnation int, writeset WriteSet)
	InvalidateWriteset(index int, incarnation int)
	SetEstimatedWriteset(index int, incarnation int, writeset WriteSet)
	GetAllWritesetKeys() map[int][]string
	CollectIteratorItems(index int) *db.MemDB
	SetReadset(index int, readset ReadSet)
	GetReadset(index int) ReadSet
	ClearReadset(index int)
	VersionedIndexedStore(index int, incarnation int, abortChannel chan occ.Abort) *VersionIndexedStore
	SetIterateset(index int, iterateset Iterateset)
	GetIterateset(index int) Iterateset
	ClearIterateset(index int)
	ValidateTransactionState(index int) (bool, []int)
}

type WriteSet map[string][]byte
type ReadSet map[string][][]byte
type Iterateset []*iterationTracker

var _ MultiVersionStore = (*Store)(nil)

type Store struct {
	multiVersionMap *sync.Map // key string -> MultiVersionValue (lock-free reads for OCC workers)

	writesetKeysMtx sync.RWMutex
	txWritesetKeys  map[int][]string

	readSetsMtx sync.RWMutex
	txReadSets  map[int]ReadSet

	iterateSetsMtx sync.RWMutex
	txIterateSets  map[int]Iterateset

	parentStore types.KVStore
}

func NewMultiVersionStore(parentStore types.KVStore) *Store {
	return &Store{
		multiVersionMap: &sync.Map{},
		txWritesetKeys:  make(map[int][]string),
		txReadSets:      make(map[int]ReadSet),
		txIterateSets:   make(map[int]Iterateset),
		parentStore:     parentStore,
	}
}

// VersionedIndexedStore creates a new versioned index store for a given incarnation and transaction index
func (s *Store) VersionedIndexedStore(index int, incarnation int, abortChannel chan occ.Abort) *VersionIndexedStore {
	return NewVersionIndexedStore(s.parentStore, s, index, incarnation, abortChannel)
}

// GetLatest implements MultiVersionStore.
func (s *Store) GetLatest(key []byte) (value MultiVersionValueItem) {
	keyString := string(key)
	mvVal, found := s.multiVersionMap.Load(keyString)
	// if the key doesn't exist in the overall map, return nil
	if !found {
		return nil
	}
	latestVal, found := mvVal.(MultiVersionValue).GetLatest()
	if !found {
		return nil // this is possible IF there is are writeset that are then removed for that key
	}
	return latestVal
}

// GetLatestBeforeIndex implements MultiVersionStore.
func (s *Store) GetLatestBeforeIndex(index int, key []byte) (value MultiVersionValueItem) {
	keyString := string(key)
	mvVal, found := s.multiVersionMap.Load(keyString)
	// if the key doesn't exist in the overall map, return nil
	if !found {
		return nil
	}
	val, found := mvVal.(MultiVersionValue).GetLatestBeforeIndex(index)
	// otherwise, we may have found a value for that key, but its not written before the index passed in
	if !found {
		return nil
	}
	// found a value prior to the passed in index, return that value (could be estimate OR deleted, but it is a definitive value)
	return val
}

// Has implements MultiVersionStore. It checks if the key exists in the multiversion store at or before the specified index.
func (s *Store) Has(index int, key []byte) bool {
	keyString := string(key)
	mvVal, found := s.multiVersionMap.Load(keyString)
	// if the key doesn't exist in the overall map, return nil
	if !found {
		return false // this is okay because the caller of this will THEN need to access the parent store to verify that the key doesnt exist there
	}
	_, foundVal := mvVal.(MultiVersionValue).GetLatestBeforeIndex(index)
	return foundVal
}

// removeOldWriteset must be called with writesetKeysMtx held for writing.
func (s *Store) removeOldWriteset(index int, newWriteSet WriteSet) {
	writeset := make(map[string][]byte)
	if newWriteSet != nil {
		// if non-nil writeset passed in, we can use that to optimize removals
		writeset = newWriteSet
	}
	// if there is already a writeset existing, we should remove that fully
	keys, loaded := s.txWritesetKeys[index]
	if loaded {
		delete(s.txWritesetKeys, index)
		// we need to delete all of the keys in the writeset from the multiversion store
		for _, key := range keys {
			// small optimization to check if the new writeset is going to write this key, if so, we can leave it behind
			if _, ok := writeset[key]; ok {
				// we don't need to remove this key because it will be overwritten anyways - saves the operation of removing + rebalancing underlying btree
				continue
			}
			// remove from the appropriate item if present in multiVersionMap
			mvVal, found := s.multiVersionMap.Load(key)
			// if the key doesn't exist in the overall map, continue
			if !found {
				continue
			}
			mvVal.(MultiVersionValue).Remove(index)
		}
	}
}

// SetWriteset sets a writeset for a transaction index, and also writes all of the multiversion items in the writeset to the multiversion store.
func (s *Store) SetWriteset(index int, incarnation int, writeset WriteSet) {
	s.writesetKeysMtx.Lock()
	s.removeOldWriteset(index, writeset)
	s.writesetKeysMtx.Unlock()

	writeSetKeys := make([]string, 0, len(writeset))
	for key, value := range writeset {
		writeSetKeys = append(writeSetKeys, key)
		loadVal, _ := s.multiVersionMap.LoadOrStore(key, NewMultiVersionItem())
		mvVal := loadVal.(MultiVersionValue)
		if value == nil {
			mvVal.Delete(index, incarnation)
		} else {
			mvVal.Set(index, incarnation, value)
		}
	}
	sort.Strings(writeSetKeys)
	s.writesetKeysMtx.Lock()
	s.txWritesetKeys[index] = writeSetKeys
	s.writesetKeysMtx.Unlock()
}

// InvalidateWriteset iterates over the keys for the given index and incarnation writeset and replaces with ESTIMATEs
func (s *Store) InvalidateWriteset(index int, incarnation int) {
	s.writesetKeysMtx.RLock()
	keys, found := s.txWritesetKeys[index]
	s.writesetKeysMtx.RUnlock()
	if !found {
		return
	}
	for _, key := range keys {
		val, _ := s.multiVersionMap.LoadOrStore(key, NewMultiVersionItem())
		val.(MultiVersionValue).SetEstimate(index, incarnation)
	}
	// we leave the writeset in place because we'll need it for key removal later if/when we replace with a new writeset
}

// SetEstimatedWriteset is used to directly write estimates instead of writing a writeset and later invalidating
func (s *Store) SetEstimatedWriteset(index int, incarnation int, writeset WriteSet) {
	s.writesetKeysMtx.Lock()
	s.removeOldWriteset(index, writeset)
	s.writesetKeysMtx.Unlock()

	writeSetKeys := make([]string, 0, len(writeset))
	// still need to save the writeset so we can remove the elements later:
	for key := range writeset {
		writeSetKeys = append(writeSetKeys, key)

		mvVal, _ := s.multiVersionMap.LoadOrStore(key, NewMultiVersionItem())
		mvVal.(MultiVersionValue).SetEstimate(index, incarnation)
	}
	sort.Strings(writeSetKeys)
	s.writesetKeysMtx.Lock()
	s.txWritesetKeys[index] = writeSetKeys
	s.writesetKeysMtx.Unlock()
}

// GetAllWritesetKeys implements MultiVersionStore.
func (s *Store) GetAllWritesetKeys() map[int][]string {
	s.writesetKeysMtx.RLock()
	writesetKeys := make(map[int][]string, len(s.txWritesetKeys))
	for index, keys := range s.txWritesetKeys {
		writesetKeys[index] = keys
	}
	s.writesetKeysMtx.RUnlock()
	return writesetKeys
}

func (s *Store) SetReadset(index int, readset ReadSet) {
	s.readSetsMtx.Lock()
	s.txReadSets[index] = readset
	s.readSetsMtx.Unlock()
}

func (s *Store) GetReadset(index int) ReadSet {
	s.readSetsMtx.RLock()
	readset, found := s.txReadSets[index]
	s.readSetsMtx.RUnlock()
	if !found {
		return nil
	}
	return readset
}

func (s *Store) SetIterateset(index int, iterateset Iterateset) {
	s.iterateSetsMtx.Lock()
	s.txIterateSets[index] = iterateset
	s.iterateSetsMtx.Unlock()
}

func (s *Store) GetIterateset(index int) Iterateset {
	s.iterateSetsMtx.RLock()
	iterateset, found := s.txIterateSets[index]
	s.iterateSetsMtx.RUnlock()
	if !found {
		return nil
	}
	return iterateset
}

func (s *Store) ClearReadset(index int) {
	s.readSetsMtx.Lock()
	delete(s.txReadSets, index)
	s.readSetsMtx.Unlock()
}

func (s *Store) ClearIterateset(index int) {
	s.iterateSetsMtx.Lock()
	delete(s.txIterateSets, index)
	s.iterateSetsMtx.Unlock()
}

// CollectIteratorItems implements MultiVersionStore. It will return a memDB containing all of the keys present in the multiversion store within the iteration range prior to (exclusive of) the index.
func (s *Store) CollectIteratorItems(index int) *db.MemDB {
	sortedItems := db.NewMemDB()

	s.writesetKeysMtx.RLock()
	// get all writeset keys prior to index
	for i := 0; i < index; i++ {
		indexedWriteset, found := s.txWritesetKeys[i]
		if !found {
			continue
		}
		for _, key := range indexedWriteset {
			sortedItems.Set([]byte(key), []byte{})
		}
	}
	s.writesetKeysMtx.RUnlock()
	return sortedItems
}

func (s *Store) validateIterator(index int, tracker iterationTracker) bool {
	// collect items from multiversion store
	sortedItems := s.CollectIteratorItems(index)
	// add the iterationtracker writeset keys to the sorted items
	for key := range tracker.writeset {
		sortedItems.Set([]byte(key), []byte{})
	}
	validChannel := make(chan bool, 1)
	abortChannel := make(chan occtypes.Abort, 1)

	// listen for abort while iterating
	go func(iterationTracker iterationTracker, items *db.MemDB, returnChan chan bool, abortChan chan occtypes.Abort) {
		var parentIter types.Iterator
		expectedKeys := iterationTracker.iteratedKeys
		foundKeys := 0
		iter := s.newMVSValidationIterator(index, iterationTracker.startKey, iterationTracker.endKey, items, iterationTracker.ascending, iterationTracker.writeset, abortChan)
		if iterationTracker.ascending {
			parentIter = s.parentStore.Iterator(iterationTracker.startKey, iterationTracker.endKey)
		} else {
			parentIter = s.parentStore.ReverseIterator(iterationTracker.startKey, iterationTracker.endKey)
		}
		// create a new MVSMergeiterator
		mergeIterator := NewMVSMergeIterator(parentIter, iter, iterationTracker.ascending, NoOpHandler{})
		defer mergeIterator.Close()
		for ; mergeIterator.Valid(); mergeIterator.Next() {
			if (len(expectedKeys) - foundKeys) == 0 {
				// if we have no more expected keys, then the iterator is invalid
				returnChan <- false
				return
			}
			key := mergeIterator.Key()
			// TODO: is this ok to not delete the key since we shouldnt have duplicate keys?
			if _, ok := expectedKeys[string(key)]; !ok {
				// if key isn't found
				returnChan <- false
				return
			}
			// remove from expected keys
			foundKeys += 1
			// delete(expectedKeys, string(key))

			// if our iterator key was the early stop, then we can break
			if bytes.Equal(key, iterationTracker.earlyStopKey) {
				break
			}
		}
		// return whether we found the exact number of expected keys
		returnChan <- !((len(expectedKeys) - foundKeys) > 0)
	}(tracker, sortedItems, validChannel, abortChannel)
	select {
	case <-abortChannel:
		// if we get an abort, then we know that the iterator is invalid
		return false
	case valid := <-validChannel:
		return valid
	}
}

func (s *Store) checkIteratorAtIndex(index int) bool {
	valid := true
	s.iterateSetsMtx.RLock()
	iterateset, found := s.txIterateSets[index]
	s.iterateSetsMtx.RUnlock()
	if !found {
		return true
	}
	for _, iterationTracker := range iterateset {
		iteratorValid := s.validateIterator(index, *iterationTracker)
		valid = valid && iteratorValid
	}
	return valid
}

func (s *Store) checkReadsetAtIndex(index int) (bool, []int) {
	conflictSet := make(map[int]struct{})
	valid := true

	s.readSetsMtx.RLock()
	readset, found := s.txReadSets[index]
	s.readSetsMtx.RUnlock()
	if !found {
		return true, []int{}
	}
	// iterate over readset and check if the value is the same as the latest value relateive to txIndex in the multiversion store
	for key, valueArr := range readset {
		if len(valueArr) != 1 {
			valid = false
			continue
		}
		value := valueArr[0]
		// get the latest value from the multiversion store
		latestValue := s.GetLatestBeforeIndex(index, []byte(key))
		if latestValue == nil {
			// this is possible if we previously read a value from a transaction write that was later reverted, so this time we read from parent store
			parentVal := s.parentStore.Get([]byte(key))
			if !bytes.Equal(parentVal, value) {
				valid = false
			}
		} else {
			// if estimate, mark as conflict index - but don't invalidate
			if latestValue.IsEstimate() {
				conflictSet[latestValue.Index()] = struct{}{}
			} else if latestValue.IsDeleted() {
				if value != nil {
					// conflict
					// TODO: would we want to return early?
					conflictSet[latestValue.Index()] = struct{}{}
					valid = false
				}
			} else if !bytes.Equal(latestValue.Value(), value) {
				conflictSet[latestValue.Index()] = struct{}{}
				valid = false
			}
		}
	}

	conflictIndices := make([]int, 0, len(conflictSet))
	for index := range conflictSet {
		conflictIndices = append(conflictIndices, index)
	}

	sort.Ints(conflictIndices)

	return valid, conflictIndices
}

// TODO: do we want to return bool + []int where bool indicates whether it was valid and then []int indicates only ones for which we need to wait due to estimates? - yes i think so?
func (s *Store) ValidateTransactionState(index int) (bool, []int) {
	// defer telemetry.MeasureSince(time.Now(), "store", "mvs", "validate")

	// TODO: can we parallelize for all iterators?
	iteratorValid := s.checkIteratorAtIndex(index)

	readsetValid, conflictIndices := s.checkReadsetAtIndex(index)

	return iteratorValid && readsetValid, conflictIndices
}

func (s *Store) WriteLatestToStore() {
	// sort the keys
	keys := []string{}
	s.multiVersionMap.Range(func(key, value interface{}) bool {
		keys = append(keys, key.(string))
		return true
	})
	sort.Strings(keys)

	for _, key := range keys {
		val, ok := s.multiVersionMap.Load(key)
		if !ok {
			continue
		}
		mvValue, found := val.(MultiVersionValue).GetLatestNonEstimate()
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
			s.parentStore.Delete([]byte(key))
			continue
		}
		if mvValue.Value() != nil {
			s.parentStore.Set([]byte(key), mvValue.Value())
		}
	}
}
