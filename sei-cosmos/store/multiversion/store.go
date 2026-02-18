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

// mvShardCount is the number of shards for the multiVersionMap.
// Must be a power of two so the mask works correctly.
const mvShardCount = 64

// mvShard is a single shard of the multiversion map, using a plain map
// guarded by a mutex. This avoids sync.Map's internal hash-trie node
// allocations (~5.4 GB per 120s benchmark with sync.Map).
type mvMapShard struct {
	mu sync.RWMutex
	m  map[string]MultiVersionValue
}

type Store struct {
	// Sharded maps that store the key string -> MultiVersionValue mapping.
	// Sharding reduces mutex contention when 24 OCC workers write
	// concurrently.
	mvShards [mvShardCount]mvMapShard

	// Per-tx data indexed by tx absolute index. Pre-allocated as slices
	// instead of sync.Map to avoid internal hash-trie node allocations
	// (~90 sync.Map instances eliminated per block across all stores).
	txWritesetKeys [][]string   // tx index -> writeset keys
	txReadSets     []ReadSet    // tx index -> readset
	txIterateSets  []Iterateset // tx index -> iterateset

	parentStore types.KVStore
}

// mvShardIdx returns the shard index for a given key using FNV-1a hash.
func mvShardIdx(key string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return h & (mvShardCount - 1)
}

func NewMultiVersionStore(parentStore types.KVStore) *Store {
	return NewMultiVersionStoreWithTxCount(parentStore, 0)
}

func NewMultiVersionStoreWithTxCount(parentStore types.KVStore, txCount int) *Store {
	s := &Store{
		txWritesetKeys: make([][]string, txCount),
		txReadSets:     make([]ReadSet, txCount),
		txIterateSets:  make([]Iterateset, txCount),
		parentStore:    parentStore,
	}
	for i := range s.mvShards {
		s.mvShards[i].m = make(map[string]MultiVersionValue)
	}
	return s
}

// growSlices ensures per-tx slices can hold index. Called only on the
// fallback path when NewMultiVersionStore was used without a tx count.
func (s *Store) growSlices(index int) {
	need := index + 1
	if need <= len(s.txWritesetKeys) {
		return
	}
	s.txWritesetKeys = append(s.txWritesetKeys, make([][]string, need-len(s.txWritesetKeys))...)
	s.txReadSets = append(s.txReadSets, make([]ReadSet, need-len(s.txReadSets))...)
	s.txIterateSets = append(s.txIterateSets, make([]Iterateset, need-len(s.txIterateSets))...)
}

// VersionedIndexedStore creates a new versioned index store for a given incarnation and transaction index
func (s *Store) VersionedIndexedStore(index int, incarnation int, abortChannel chan occ.Abort) *VersionIndexedStore {
	s.growSlices(index)
	return NewVersionIndexedStore(s.parentStore, s, index, incarnation, abortChannel)
}

// GetLatest implements MultiVersionStore.
func (s *Store) GetLatest(key []byte) (value MultiVersionValueItem) {
	keyString := string(key)
	shard := &s.mvShards[mvShardIdx(keyString)]
	shard.mu.RLock()
	mvVal, found := shard.m[keyString]
	shard.mu.RUnlock()
	if !found {
		return nil
	}
	latestVal, found := mvVal.GetLatest()
	if !found {
		return nil
	}
	return latestVal
}

// GetLatestBeforeIndex implements MultiVersionStore.
func (s *Store) GetLatestBeforeIndex(index int, key []byte) (value MultiVersionValueItem) {
	keyString := string(key)
	shard := &s.mvShards[mvShardIdx(keyString)]
	shard.mu.RLock()
	mvVal, found := shard.m[keyString]
	shard.mu.RUnlock()
	if !found {
		return nil
	}
	val, found := mvVal.GetLatestBeforeIndex(index)
	if !found {
		return nil
	}
	return val
}

// Has implements MultiVersionStore. It checks if the key exists in the multiversion store at or before the specified index.
func (s *Store) Has(index int, key []byte) bool {
	keyString := string(key)
	shard := &s.mvShards[mvShardIdx(keyString)]
	shard.mu.RLock()
	mvVal, found := shard.m[keyString]
	shard.mu.RUnlock()
	if !found {
		return false
	}
	_, foundVal := mvVal.GetLatestBeforeIndex(index)
	return foundVal
}

func (s *Store) removeOldWriteset(index int, newWriteSet WriteSet) {
	writeset := make(map[string][]byte)
	if newWriteSet != nil {
		writeset = newWriteSet
	}
	if index >= len(s.txWritesetKeys) {
		return
	}
	keys := s.txWritesetKeys[index]
	s.txWritesetKeys[index] = nil
	if keys != nil {
		for _, key := range keys {
			if _, ok := writeset[key]; ok {
				continue
			}
			shard := &s.mvShards[mvShardIdx(key)]
			shard.mu.RLock()
			mvVal, found := shard.m[key]
			shard.mu.RUnlock()
			if !found {
				continue
			}
			mvVal.Remove(index)
		}
	}
}

// SetWriteset sets a writeset for a transaction index, and also writes all of the multiversion items in the writeset to the multiversion store.
func (s *Store) SetWriteset(index int, incarnation int, writeset WriteSet) {
	s.growSlices(index)
	s.removeOldWriteset(index, writeset)

	writeSetKeys := make([]string, 0, len(writeset))
	for key, value := range writeset {
		writeSetKeys = append(writeSetKeys, key)
		shard := &s.mvShards[mvShardIdx(key)]
		shard.mu.Lock()
		mvVal, ok := shard.m[key]
		if !ok {
			mvVal = NewMultiVersionItem()
			shard.m[key] = mvVal
		}
		shard.mu.Unlock()
		if value == nil {
			mvVal.Delete(index, incarnation)
		} else {
			mvVal.Set(index, incarnation, value)
		}
	}
	sort.Strings(writeSetKeys)
	s.txWritesetKeys[index] = writeSetKeys
}

// InvalidateWriteset iterates over the keys for the given index and incarnation writeset and replaces with ESTIMATEs
func (s *Store) InvalidateWriteset(index int, incarnation int) {
	if index >= len(s.txWritesetKeys) {
		return
	}
	keys := s.txWritesetKeys[index]
	if keys == nil {
		return
	}
	for _, key := range keys {
		shard := &s.mvShards[mvShardIdx(key)]
		shard.mu.Lock()
		mvVal, ok := shard.m[key]
		if !ok {
			mvVal = NewMultiVersionItem()
			shard.m[key] = mvVal
		}
		shard.mu.Unlock()
		mvVal.SetEstimate(index, incarnation)
	}
}

// SetEstimatedWriteset is used to directly write estimates instead of writing a writeset and later invalidating
func (s *Store) SetEstimatedWriteset(index int, incarnation int, writeset WriteSet) {
	s.growSlices(index)
	s.removeOldWriteset(index, writeset)

	writeSetKeys := make([]string, 0, len(writeset))
	for key := range writeset {
		writeSetKeys = append(writeSetKeys, key)

		shard := &s.mvShards[mvShardIdx(key)]
		shard.mu.Lock()
		mvVal, ok := shard.m[key]
		if !ok {
			mvVal = NewMultiVersionItem()
			shard.m[key] = mvVal
		}
		shard.mu.Unlock()
		mvVal.SetEstimate(index, incarnation)
	}
	sort.Strings(writeSetKeys)
	s.txWritesetKeys[index] = writeSetKeys
}

// GetAllWritesetKeys implements MultiVersionStore.
func (s *Store) GetAllWritesetKeys() map[int][]string {
	writesetKeys := make(map[int][]string)
	for i, keys := range s.txWritesetKeys {
		if keys != nil {
			writesetKeys[i] = keys
		}
	}
	return writesetKeys
}

func (s *Store) SetReadset(index int, readset ReadSet) {
	s.growSlices(index)
	s.txReadSets[index] = readset
}

func (s *Store) GetReadset(index int) ReadSet {
	if index >= len(s.txReadSets) {
		return nil
	}
	return s.txReadSets[index]
}

func (s *Store) SetIterateset(index int, iterateset Iterateset) {
	s.growSlices(index)
	s.txIterateSets[index] = iterateset
}

func (s *Store) GetIterateset(index int) Iterateset {
	if index >= len(s.txIterateSets) {
		return nil
	}
	return s.txIterateSets[index]
}

func (s *Store) ClearReadset(index int) {
	if index < len(s.txReadSets) {
		s.txReadSets[index] = nil
	}
}

func (s *Store) ClearIterateset(index int) {
	if index < len(s.txIterateSets) {
		s.txIterateSets[index] = nil
	}
}

// CollectIteratorItems implements MultiVersionStore. It will return a memDB containing all of the keys present in the multiversion store within the iteration range prior to (exclusive of) the index.
func (s *Store) CollectIteratorItems(index int) *db.MemDB {
	sortedItems := db.NewMemDB()

	// get all writeset keys prior to index
	limit := index
	if limit > len(s.txWritesetKeys) {
		limit = len(s.txWritesetKeys)
	}
	for i := 0; i < limit; i++ {
		indexedWriteset := s.txWritesetKeys[i]
		if indexedWriteset == nil {
			continue
		}
		for _, key := range indexedWriteset {
			sortedItems.Set([]byte(key), []byte{})
		}
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
	if index >= len(s.txIterateSets) {
		return true
	}
	iterateset := s.txIterateSets[index]
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

	if index >= len(s.txReadSets) {
		return true, []int{}
	}
	readset := s.txReadSets[index]
	if readset == nil {
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
	// collect all keys from all shards
	keys := make([]string, 0, 256)
	for i := range s.mvShards {
		shard := &s.mvShards[i]
		shard.mu.RLock()
		for k := range shard.m {
			keys = append(keys, k)
		}
		shard.mu.RUnlock()
	}
	sort.Strings(keys)

	for _, key := range keys {
		shard := &s.mvShards[mvShardIdx(key)]
		shard.mu.RLock()
		mvVal := shard.m[key]
		shard.mu.RUnlock()
		mvValue, found := mvVal.GetLatestNonEstimate()
		if !found {
			continue
		}
		if mvValue.IsEstimate() {
			panic("should not have any estimate values when writing to parent store")
		}
		if mvValue.IsDeleted() {
			s.parentStore.Delete([]byte(key))
			continue
		}
		if mvValue.Value() != nil {
			s.parentStore.Set([]byte(key), mvValue.Value())
		}
	}
}
