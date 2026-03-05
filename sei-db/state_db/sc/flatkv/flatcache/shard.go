package flatcache

import (
	"context"
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-iavl/proto"
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

	// A scheduler for asyncronous reads.
	readScheduler *readScheduler

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
	// We are aware that the value is deleted (special case of data being avialable).
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
func NewShard(readScheduler *readScheduler, maxSize int) (*shard, error) {

	if maxSize <= 0 {
		return nil, fmt.Errorf("maxSize must be greater than 0")
	}

	return &shard{
		readScheduler: readScheduler,
		lock:          sync.Mutex{},
	}, nil
}

// Get returns the value for the given key, or (nil, false) if not found.
func (s *shard) Get(key []byte) ([]byte, bool, error) {
	s.lock.Lock()

	entry := s.getEntry(key)

	switch entry.status {

	case statusAvailable:
		value := entry.value
		s.gcQueue.Touch(key)
		s.lock.Unlock()
		return value, true, nil
	case statusDeleted:
		s.gcQueue.Touch(key)
		s.lock.Unlock()
		return nil, false, nil
	case statusScheduled:
		// Another goroutine initiated a read, wait for that read to finish.
		valueChan := entry.valueChan
		s.lock.Unlock()
		value := <-valueChan
		valueChan <- value // reload the channel in case there are other listeners
		return value, value != nil, nil
	case statusUnknown:
		// We are the first goroutine to read this value.
		entry.status = statusScheduled
		valueChan := make(chan []byte, 1)
		entry.valueChan = valueChan
		s.readScheduler.ScheduleRead(key, entry)
		s.lock.Unlock()
		value := <-valueChan
		valueChan <- value // reload the channel in case there are other listeners
		return value, value != nil, nil
	default:
		panic(fmt.Sprintf("unexpected statustatus: %#v", entry.status))
	}
}

// This method is called by the read scheduler when a value becomes available.
func (se *shardEntry) InjectValue(key []byte, value []byte) {
	se.shard.lock.Lock()

	if value == nil {
		se.status = statusDeleted
	} else {
		se.status = statusAvailable
		se.value = value
	}

	se.shard.gcQueue.Push(key, len(key)+len(value))

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
func (s *shard) BatchSet(entries []*proto.KVPair) {
	s.lock.Lock()
	for _, entry := range entries {
		if entry.Delete {
			s.deleteUnlocked(entry.Key)
		} else {
			s.setUnlocked(entry.Key, entry.Value)
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
func (s *shard) RunGarbageCollection() {
	s.lock.Lock()

	for s.gcQueue.GetTotalSize() > s.maxSize {
		next := s.gcQueue.PopLeastRecentlyUsed()
		delete(s.data, string(next)) // TODO use unsafe copy
	}

	s.lock.Unlock()
}
