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
	ReleaseVersionIndexedStore(vis *VersionIndexedStore)
	SetIterateset(index int, iterateset Iterateset)
	GetIterateset(index int) Iterateset
	ClearIterateset(index int)
	ValidateTransactionState(index int) (bool, []int)
}

type WriteSet map[string][]byte

// ReadSetEntry stores a single read value per key. If the same key is read
// with different values, Conflict is set to true (indicating validation failure).
type ReadSetEntry struct {
	Value    []byte
	Conflict bool
}

type ReadSet map[string]ReadSetEntry
type Iterateset []*iterationTracker

var _ MultiVersionStore = (*Store)(nil)

// txSlot holds per-transaction data with its own lock to eliminate cross-tx contention.
type txSlot struct {
	mu           sync.RWMutex
	writesetKeys []string
	readset      ReadSet
	iterateset   Iterateset
}

type Store struct {
	multiVersionMap *sync.Map // key string -> MultiVersionValue (lock-free reads for OCC workers)

	txSlots []txSlot // pre-allocated, indexed by tx index

	parentStore types.KVStore

	visPool sync.Pool // pools *VersionIndexedStore for reuse
}

func NewMultiVersionStore(parentStore types.KVStore) *Store {
	return NewMultiVersionStoreWithSize(parentStore, 16)
}

func NewMultiVersionStoreWithSize(parentStore types.KVStore, numTxs int) *Store {
	if numTxs < 1 {
		numTxs = 16
	}
	return &Store{
		multiVersionMap: &sync.Map{},
		txSlots:         make([]txSlot, numTxs),
		parentStore:     parentStore,
	}
}

// slot returns the txSlot for the given index. Panics if index is out of bounds.
// In production, NewMultiVersionStoreWithSize is called with a sufficient size.
// In tests, NewMultiVersionStore defaults to 16 slots which covers typical test indices.
func (s *Store) slot(index int) *txSlot {
	return &s.txSlots[index]
}

// VersionedIndexedStore creates a new versioned index store for a given incarnation and transaction index.
// It reuses pooled instances when available to reduce allocations.
func (s *Store) VersionedIndexedStore(index int, incarnation int, abortChannel chan occ.Abort) *VersionIndexedStore {
	if v := s.visPool.Get(); v != nil {
		vis := v.(*VersionIndexedStore)
		vis.Reset(s.parentStore, s, index, incarnation, abortChannel)
		return vis
	}
	return NewVersionIndexedStore(s.parentStore, s, index, incarnation, abortChannel)
}

// ReleaseVersionIndexedStore returns a VersionIndexedStore to the pool for reuse.
func (s *Store) ReleaseVersionIndexedStore(vis *VersionIndexedStore) {
	s.visPool.Put(vis)
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

// removeOldWriteset must be called with the slot's mu held for writing.
func (s *Store) removeOldWriteset(sl *txSlot, index int, newWriteSet WriteSet) {
	writeset := make(map[string][]byte)
	if newWriteSet != nil {
		// if non-nil writeset passed in, we can use that to optimize removals
		writeset = newWriteSet
	}
	// if there is already a writeset existing, we should remove that fully
	keys := sl.writesetKeys
	if len(keys) > 0 {
		sl.writesetKeys = nil
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
	sl := s.slot(index)
	sl.mu.Lock()
	s.removeOldWriteset(sl, index, writeset)
	sl.mu.Unlock()

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
	sl.mu.Lock()
	sl.writesetKeys = writeSetKeys
	sl.mu.Unlock()
}

// InvalidateWriteset iterates over the keys for the given index and incarnation writeset and replaces with ESTIMATEs
func (s *Store) InvalidateWriteset(index int, incarnation int) {
	sl := s.slot(index)
	sl.mu.RLock()
	keys := sl.writesetKeys
	sl.mu.RUnlock()
	if len(keys) == 0 {
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
	sl := s.slot(index)
	sl.mu.Lock()
	s.removeOldWriteset(sl, index, writeset)
	sl.mu.Unlock()

	writeSetKeys := make([]string, 0, len(writeset))
	// still need to save the writeset so we can remove the elements later:
	for key := range writeset {
		writeSetKeys = append(writeSetKeys, key)

		mvVal, _ := s.multiVersionMap.LoadOrStore(key, NewMultiVersionItem())
		mvVal.(MultiVersionValue).SetEstimate(index, incarnation)
	}
	sl.mu.Lock()
	sl.writesetKeys = writeSetKeys
	sl.mu.Unlock()
}

// GetAllWritesetKeys implements MultiVersionStore.
func (s *Store) GetAllWritesetKeys() map[int][]string {
	writesetKeys := make(map[int][]string)
	for i := range s.txSlots {
		sl := &s.txSlots[i]
		sl.mu.RLock()
		if len(sl.writesetKeys) > 0 {
			writesetKeys[i] = sl.writesetKeys
		}
		sl.mu.RUnlock()
	}
	return writesetKeys
}

func (s *Store) SetReadset(index int, readset ReadSet) {
	// Clone the readset so the caller (VIS) can reuse its map via clear().
	clone := make(ReadSet, len(readset))
	for k, v := range readset {
		clone[k] = v
	}
	sl := s.slot(index)
	sl.mu.Lock()
	sl.readset = clone
	sl.mu.Unlock()
}

func (s *Store) GetReadset(index int) ReadSet {
	sl := s.slot(index)
	sl.mu.RLock()
	readset := sl.readset
	sl.mu.RUnlock()
	return readset
}

func (s *Store) SetIterateset(index int, iterateset Iterateset) {
	// Clone the iterateset so the caller (VIS) can reuse its slice via [:0].
	clone := make(Iterateset, len(iterateset))
	copy(clone, iterateset)
	sl := s.slot(index)
	sl.mu.Lock()
	sl.iterateset = clone
	sl.mu.Unlock()
}

func (s *Store) GetIterateset(index int) Iterateset {
	sl := s.slot(index)
	sl.mu.RLock()
	iterateset := sl.iterateset
	sl.mu.RUnlock()
	return iterateset
}

func (s *Store) ClearReadset(index int) {
	sl := s.slot(index)
	sl.mu.Lock()
	sl.readset = nil
	sl.mu.Unlock()
}

func (s *Store) ClearIterateset(index int) {
	sl := s.slot(index)
	sl.mu.Lock()
	sl.iterateset = nil
	sl.mu.Unlock()
}

// CollectIteratorItems implements MultiVersionStore. It will return a memDB containing all of the keys present in the multiversion store within the iteration range prior to (exclusive of) the index.
func (s *Store) CollectIteratorItems(index int) *db.MemDB {
	sortedItems := db.NewMemDB()

	// get all writeset keys prior to index
	limit := index
	if limit > len(s.txSlots) {
		limit = len(s.txSlots)
	}
	for i := 0; i < limit; i++ {
		sl := &s.txSlots[i]
		sl.mu.RLock()
		for _, key := range sl.writesetKeys {
			sortedItems.Set([]byte(key), []byte{})
		}
		sl.mu.RUnlock()
	}
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
	iterateset := s.GetIterateset(index)
	if iterateset == nil {
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

	readset := s.GetReadset(index)
	if readset == nil {
		return true, []int{}
	}
	// iterate over readset and check if the value is the same as the latest value relative to txIndex in the multiversion store
	for key, entry := range readset {
		if entry.Conflict {
			valid = false
			continue
		}
		value := entry.Value
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
