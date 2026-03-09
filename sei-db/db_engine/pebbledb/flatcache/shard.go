package flatcache

import (
	"context"
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// TODO unsafe byte-> string conversion maybe?

// A single shard of a Cache.
type shard struct {
	ctx context.Context

	// A lock to protect the shard's data.
	lock sync.Mutex

	// The data in the shard.
	data map[string]*shardEntry

	// Organizes data for garbage collection.
	gcQueue *lruQueue

	// A pool for asynchronous reads.
	readPool threading.Pool

	// A function that reads a value from the database.
	readFunc func(key []byte) []byte

	// The maximum size of this cache, in bytes.
	maxSize int
}

// The status of a value in the cache.
type valueStatus int

const (
	// The value is not known and we are not currently attempting to find it.
	statusUnknown valueStatus = iota
	// We've scheduled a read of the value but haven't yet finsihed the read.
	statusScheduled
	// The data is available.
	statusAvailable
	// We are aware that the value is deleted (special case of data being available).
	statusDeleted
)

// A single shardEntry in a shard. Records data for a single key.
type shardEntry struct {
	// The parent shard that contains this entry.
	shard *shard

	// The current status of this entry.
	status valueStatus

	// The value, if known.
	value []byte

	// If the value is not available when we request it,
	// it will be written to this channel when it is available.
	valueChan chan []byte
}

// Creates a new Shard.
func NewShard(
	ctx context.Context,
	readPool threading.Pool,
	readFunc func(key []byte) []byte,
	maxSize int,
) (*shard, error) {

	if maxSize <= 0 {
		return nil, fmt.Errorf("maxSize must be greater than 0")
	}

	return &shard{
		ctx:      ctx,
		readPool: readPool,
		readFunc: readFunc,
		lock:     sync.Mutex{},
		data:     make(map[string]*shardEntry),
		gcQueue:  NewLRUQueue(),
		maxSize:  maxSize,
	}, nil
}

// Get returns the value for the given key, or (nil, false) if not found.
func (s *shard) Get(key []byte, updateLru bool) ([]byte, bool, error) {
	s.lock.Lock()

	entry := s.getEntry(key)

	switch entry.status {

	case statusAvailable:
		value := entry.value
		if updateLru {
			s.gcQueue.Touch(key)
		}
		s.lock.Unlock()
		return value, true, nil
	case statusDeleted:
		if updateLru {
			s.gcQueue.Touch(key)
		}
		s.lock.Unlock()
		return nil, false, nil
	case statusScheduled:
		// Another goroutine initiated a read, wait for that read to finish.
		valueChan := entry.valueChan
		s.lock.Unlock()
		value, err := threading.InterruptiblePull(s.ctx, valueChan)
		if err != nil {
			return nil, false, fmt.Errorf("failed to pull value from channel: %w", err)
		}
		valueChan <- value // reload the channel in case there are other listeners
		return value, value != nil, nil
	case statusUnknown:
		// We are the first goroutine to read this value.
		entry.status = statusScheduled
		valueChan := make(chan []byte, 1)
		entry.valueChan = valueChan
		s.lock.Unlock()
		err := s.readPool.Submit(s.ctx, func() {
			value := s.readFunc(key)
			entry.injectValue(key, value)
		})
		if err != nil {
			return nil, false, fmt.Errorf("failed to schedule read: %w", err)
		}
		value, err := threading.InterruptiblePull(s.ctx, valueChan)
		if err != nil {
			return nil, false, fmt.Errorf("failed to pull value from channel: %w", err)
		}
		valueChan <- value // reload the channel in case there are other listeners
		return value, value != nil, nil
	default:
		panic(fmt.Sprintf("unexpected status: %#v", entry.status))
	}
}

// This method is called by the read scheduler when a value becomes available.
func (se *shardEntry) injectValue(key []byte, value []byte) {
	se.shard.lock.Lock()

	if se.status == statusScheduled {
		// In the time since the read was scheduled, nobody has written to this entry,
		// so safe to overwrite the value.
		if value == nil {
			se.status = statusDeleted
			se.value = nil
			se.shard.gcQueue.Push(key, len(key))
		} else {
			se.status = statusAvailable
			se.value = value
			se.shard.gcQueue.Push(key, len(key)+len(value))
		}
	}

	se.shard.lock.Unlock()

	se.valueChan <- value
}

// Get a shard entry for a given key. Caller is responsible for holding the shard's lock
// when this method is called.
func (s *shard) getEntry(key []byte) *shardEntry {
	entry, ok := s.data[string(key)]
	if !ok {
		entry = &shardEntry{
			shard:  s,
			status: statusUnknown,
		}
		s.data[string(key)] = entry
	}
	return entry
}

// Tracks a key whose value is not yet available and must be waited on.
type pendingRead struct {
	key           string
	entry         *shardEntry
	valueChan     chan []byte
	needsSchedule bool
	// Populated after the read completes, used by bulkInjectValues.
	value []byte
}

// BatchGet reads a batch of keys from the shard. Results are written into the provided map.
func (s *shard) BatchGet(keys map[string]types.BatchGetResult) error {
	pending := make([]pendingRead, 0, len(keys))

	s.lock.Lock()
	for key := range keys {
		entry := s.getEntry([]byte(key))

		switch entry.status {
		case statusAvailable:
			keys[key] = types.BatchGetResult{Value: entry.value, Found: true}
		case statusDeleted:
			keys[key] = types.BatchGetResult{Found: false}
		case statusScheduled:
			pending = append(pending, pendingRead{
				key:       key,
				entry:     entry,
				valueChan: entry.valueChan,
			})
		case statusUnknown:
			entry.status = statusScheduled
			valueChan := make(chan []byte, 1)
			entry.valueChan = valueChan
			pending = append(pending, pendingRead{
				key:           key,
				entry:         entry,
				valueChan:     valueChan,
				needsSchedule: true,
			})
		default:
			s.lock.Unlock()
			panic(fmt.Sprintf("unexpected status: %#v", entry.status))
		}
	}
	s.lock.Unlock()

	for i := range pending {
		if pending[i].needsSchedule {
			p := &pending[i]
			err := s.readPool.Submit(s.ctx, func() {
				value := s.readFunc([]byte(p.key))
				p.entry.valueChan <- value
				// Intentionally do not call injectValue here, we want to defer the update to a single bulk operation.
			})
			if err != nil {
				return fmt.Errorf("failed to schedule read: %w", err)
			}
		}
	}

	for i := range pending {
		value, err := threading.InterruptiblePull(s.ctx, pending[i].valueChan)
		if err != nil {
			return fmt.Errorf("failed to pull value from channel: %w", err)
		}
		pending[i].valueChan <- value
		pending[i].value = value

		keys[pending[i].key] = types.BatchGetResult{Value: value, Found: value != nil}
	}

	if len(pending) > 0 {
		go s.bulkInjectValues(pending)
	}

	return nil
}

// Applies deferred cache updates for a batch of reads under a single lock acquisition.
func (s *shard) bulkInjectValues(reads []pendingRead) {
	s.lock.Lock()
	for i := range reads {
		entry := reads[i].entry
		if entry.status != statusScheduled {
			continue
		}
		if reads[i].value == nil {
			entry.status = statusDeleted
			entry.value = nil
			s.gcQueue.Push([]byte(reads[i].key), len(reads[i].key))
		} else {
			entry.status = statusAvailable
			entry.value = reads[i].value
			s.gcQueue.Push([]byte(reads[i].key), len(reads[i].key)+len(reads[i].value))
		}
	}
	s.lock.Unlock()
}

// Set sets the value for the given key.
func (s *shard) Set(key []byte, value []byte) {
	s.lock.Lock()
	s.setUnlocked(key, value)
	s.lock.Unlock()
}

// Set a value. Caller is required to hold the lock.
func (s *shard) setUnlocked(key []byte, value []byte) {
	entry := s.getEntry(key)
	entry.status = statusAvailable
	entry.value = value

	s.gcQueue.Push(key, len(key)+len(value))
}

// BatchSet sets the values for a batch of keys.
func (s *shard) BatchSet(entries []CacheUpdate) {
	s.lock.Lock()
	for i := range entries {
		if entries[i].IsDelete {
			s.deleteUnlocked(entries[i].Key)
		} else {
			s.setUnlocked(entries[i].Key, entries[i].Value)
		}
	}
	s.lock.Unlock()
}

// Delete deletes the value for the given key.
func (s *shard) Delete(key []byte) {
	s.lock.Lock()
	s.deleteUnlocked(key)
	s.lock.Unlock()
}

// Delete a value. Caller is required to hold the lock.
func (s *shard) deleteUnlocked(key []byte) {
	entry := s.getEntry(key)
	entry.status = statusDeleted
	entry.value = nil

	s.gcQueue.Push(key, len(key))
}

// RunGarbageCollection runs the garbage collection process.
func (s *shard) RunGarbageCollection() { // TODO maybe just do this after each update?
	s.lock.Lock()

	for s.gcQueue.GetTotalSize() > s.maxSize {
		next := s.gcQueue.PopLeastRecentlyUsed()
		delete(s.data, string(next)) // TODO use unsafe copy
	}

	s.lock.Unlock()
}
